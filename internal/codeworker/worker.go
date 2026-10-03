// Package codeworker is only imported by the separate worker binary. The
// service never initializes goja in its own address space.
package codeworker

import (
	"apihub-go/internal/jsonutil"
	"apihub-go/internal/workflow"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dop251/goja"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"io"
	"net"
	"os"
	"reflect"
	"strings"
	"time"
)

func Run(in io.Reader, out io.Writer) error {
	started := time.Now()
	raw, err := io.ReadAll(io.LimitReader(in, workflow.MaxCodeData+(40<<10)+1))
	if err != nil || len(raw) > workflow.MaxCodeData+(40<<10) {
		return fmt.Errorf("invalid protocol input")
	}
	var request workflow.CodeRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("multiple requests")
	}
	response := workflow.CodeResponse{Protocol: 1, RequestID: request.RequestID, Build: workflow.CodeBuild}
	finish := func(code string) error {
		response.ErrorCode = code
		response.DurationMS = time.Since(started).Milliseconds()
		return json.NewEncoder(out).Encode(response)
	}
	if request.Protocol != 1 || request.RuntimeProfile != "js-v1" {
		return finish("code_runtime_unavailable")
	}
	if err := sandboxCheck(); err != nil {
		response.Diagnostics = []workflow.Diagnostic{{Code: "code_runtime_unavailable", Phase: "code", Severity: "error", Message: err.Error()}}
		return finish("code_runtime_unavailable")
	}
	if request.Mode == "probe" {
		response.Success = true
		return finish("")
	}
	if len(request.Code) > 32<<10 {
		return finish("code_compile_error")
	}
	tree, err := parser.ParseFile(nil, "code", request.Code, 0)
	if err != nil {
		if list, ok := err.(parser.ErrorList); ok && len(list) > 0 {
			response.Diagnostics = []workflow.Diagnostic{{Code: "code_compile_error", Phase: "code", Severity: "error", Message: "JavaScript syntax error", Line: list[0].Position.Line, Column: list[0].Position.Column}}
		}
		return finish("code_compile_error")
	}
	if !allowedSyntax(reflect.ValueOf(tree), map[uintptr]bool{}) {
		return finish("code_compile_error")
	}
	mainFound := false
	for _, statement := range tree.Body {
		if declaration, ok := statement.(*ast.FunctionDeclaration); ok && declaration.Function.Name != nil && declaration.Function.Name.Name.String() == "main" {
			mainFound = true
		}
	}
	if !mainFound {
		return finish("code_entrypoint_invalid")
	}
	program, err := goja.CompileAST(tree, false)
	if err != nil {
		return finish("code_compile_error")
	}
	if request.Mode == "compile" {
		response.Success = true
		return finish("")
	}
	if request.Mode != "execute" {
		return finish("code_runtime_unavailable")
	}
	if err := safeNumbers(request.Input, 1); err != nil {
		return finish("code_input_invalid")
	}
	runtimeFailure := func(err error) error {
		code := runtimeError(err)
		var exception *goja.Exception
		if errors.As(err, &exception) {
			for _, frame := range exception.Stack() {
				if frame.SrcName() == "code" {
					position := frame.Position()
					response.Diagnostics = []workflow.Diagnostic{{Code: code, Phase: "code", FieldPath: "/code", Severity: "error", Message: code, Line: position.Line, Column: position.Column}}
					break
				}
			}
		}
		return finish(code)
	}
	runtime := goja.New()
	runtime.SetMaxCallStackSize(256)
	runtime.SetTimeSource(func() time.Time { return time.Unix(0, 0).UTC() })
	runtime.SetRandSource(func() float64 { return 0.5 })
	// Capture trusted primitives before user initialization. Reject getters,
	// exotic objects, undefined, holes, cycles and unsafe numbers before JSON.
	validator, err := runtime.RunString(`(function(){
 const symbols=Object.getOwnPropertySymbols, own=Function.prototype.call.bind(Object.prototype.hasOwnProperty), string=String, descriptors=Object.getOwnPropertyDescriptors, proto=Object.getPrototypeOf, plain=Object.prototype, array=Array.isArray, keys=Object.keys, stringify=JSON.stringify, finite=Number.isFinite, integer=Number.isInteger, safe=Number.isSafeInteger, create=Object.create;
 return function(value){
  const seen=create(null); let size=0;
  function visit(v,depth){
   if(depth>16)throw Error('depth');
   if(v===null)return 'null';
   const t=typeof v;
   if(t==='string'||t==='boolean')return stringify(v);
   if(t==='number'){if(!finite(v)||(integer(v)&&!safe(v)))throw Error('number');return stringify(v);}
   if(t!=='object')throw Error('type');
   for(let i=0;i<size;i++){if(seen[i]===v)throw Error('cycle');} seen[size++]=v;
   if(symbols(v).length)throw Error('symbol');
   const isArray=array(v);
   if(!isArray&&proto(v)!==plain&&proto(v)!==null)throw Error('object');
   const ds=descriptors(v), names=keys(ds); let encoded=isArray?'[':'{', count=0;
   for(let i=0;i<names.length;i++){
    const k=names[i], d=ds[k]; if(own(d,'get')||own(d,'set'))throw Error('accessor');
    if(isArray&&k==='length')continue;
    if(isArray&&(string(count)!==k||!d.enumerable))throw Error('array');
    const field=visit(d.value,depth+1);
    if(d.enumerable){if(count++)encoded+=','; if(!isArray)encoded+=stringify(k)+':'; encoded+=field;}
   }
   if(isArray&&count!==v.length)throw Error('hole');
   size--; return encoded+(isArray?']':'}');
  }
  if(value===null||typeof value!=='object'||array(value))throw Error('root'); return visit(value,1);
 };
 })()`)
	if err != nil {
		return finish("code_worker_failed")
	}
	validate, _ := goja.AssertFunction(validator)
	_, err = runtime.RunString(`(function(){ const OriginalDate=Date; const unsupported=function(){throw Error('unsupported capability');}; Function.prototype.constructor=unsupported; Object.getPrototypeOf(function*(){}).constructor=unsupported; globalThis.eval=unsupported; globalThis.Function=unsupported; globalThis.Promise=undefined; globalThis.Proxy=undefined; globalThis.WeakRef=undefined; globalThis.SharedArrayBuffer=undefined; globalThis.Atomics=undefined; Math.random=unsupported; function FixedDate(...args){if(args.length===0)unsupported(); if(args.length===1&&typeof args[0]==='string'&&!/(Z|[+-]\d{2}:\d{2})$/.test(args[0]))unsupported(); return new OriginalDate(...args);} FixedDate.prototype=OriginalDate.prototype; FixedDate.UTC=OriginalDate.UTC; FixedDate.parse=function(text){if(!/(Z|[+-]\d{2}:\d{2})$/.test(text))unsupported();return OriginalDate.parse(text);}; FixedDate.now=unsupported; OriginalDate.prototype.constructor=FixedDate; globalThis.Date=FixedDate; })()`)
	if err != nil {
		return finish("code_worker_failed")
	}
	logBytes := 0
	_ = runtime.Set("console", map[string]any{"log": func(call goja.FunctionCall) goja.Value {
		if response.LogCount >= 100 || logBytes >= 4<<10 {
			response.LogsTruncated = true
			return goja.Undefined()
		}
		for _, arg := range call.Arguments {
			if _, object := arg.(*goja.Object); object {
				logBytes += 32
			} else {
				logBytes += len(arg.String())
			}
		}
		response.LogCount++
		if logBytes > 4<<10 {
			response.LogsTruncated = true
		}
		return goja.Undefined()
	}})
	inputJSON, _ := json.Marshal(request.Input)
	input, err := runtime.RunString("JSON.parse(" + strconvString(string(inputJSON)) + ")")
	if err != nil {
		return finish("code_input_invalid")
	}
	if _, err = runtime.RunProgram(program); err != nil {
		return runtimeFailure(err)
	}
	main, ok := goja.AssertFunction(runtime.Get("main"))
	if !ok {
		return finish("code_entrypoint_invalid")
	}
	value, err := main(goja.Undefined(), input)
	if err != nil {
		return runtimeFailure(err)
	}
	encoded, err := validate(goja.Undefined(), value)
	if err != nil {
		return finish("code_output_invalid")
	}
	output := []byte(encoded.String())
	if len(output) > workflow.MaxCodeData {
		return finish("code_output_invalid")
	}
	if err := jsonutil.Unmarshal(output, &response.Output); err != nil || response.Output == nil {
		return finish("code_output_invalid")
	}
	response.Success = true
	return finish("")
}
func strconvString(value string) string { raw, _ := json.Marshal(value); return string(raw) }
func runtimeError(err error) string {
	if _, ok := err.(*goja.StackOverflowError); ok {
		return "code_stack_limit"
	}
	return "code_execution_failed"
}
func allowedSyntax(value reflect.Value, seen map[uintptr]bool) bool {
	if !value.IsValid() {
		return true
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return true
		}
		return allowedSyntax(value.Elem(), seen)
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return true
		}
		p := value.Pointer()
		if seen[p] {
			return true
		}
		seen[p] = true
		return allowedSyntax(value.Elem(), seen)
	}
	if value.Kind() == reflect.Struct {
		if value.Type().PkgPath() == "github.com/dop251/goja/ast" {
			for i := 0; i < value.NumField(); i++ {
				name := value.Type().Field(i).Name
				if name == "Async" && value.Field(i).Kind() == reflect.Bool && value.Field(i).Bool() {
					return false
				}
			}
		}
		for i := 0; i < value.NumField(); i++ {
			if !allowedSyntax(value.Field(i), seen) {
				return false
			}
		}
	}
	if value.Kind() == reflect.Slice {
		for i := 0; i < value.Len(); i++ {
			if !allowedSyntax(value.Index(i), seen) {
				return false
			}
		}
	}
	return true
}
func safeNumbers(value any, depth int) error {
	if depth > 16 {
		return fmt.Errorf("depth")
	}
	switch v := value.(type) {
	case json.Number:
		if err := workflow.CheckJSNumber(v); err != nil {
			return fmt.Errorf("number")
		}
	case map[string]any:
		for _, item := range v {
			if err := safeNumbers(item, depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range v {
			if err := safeNumbers(item, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
func sandboxCheck() error {
	if os.Getuid() != 65534 || len(os.Environ()) != 2 || os.Getenv("TZ") != "UTC" || os.Getenv("PWD") != "/" {
		return fmt.Errorf("identity or environment")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil || !strings.Contains(string(status), "NoNewPrivs:\t1") || !strings.Contains(string(status), "CapEff:\t0000000000000000") {
		return fmt.Errorf("privileges")
	}
	memory, err := os.ReadFile("/limits/memory.max")
	if err != nil || strings.TrimSpace(string(memory)) != "134217728" {
		return fmt.Errorf("memory")
	}
	swap, err := os.ReadFile("/limits/memory.swap.max")
	if err != nil || strings.TrimSpace(string(swap)) != "0" {
		return fmt.Errorf("memory swap")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	for _, network := range interfaces {
		if network.Name != "lo" {
			return fmt.Errorf("network")
		}
	}
	if _, err := os.Stat("/home"); !os.IsNotExist(err) {
		return fmt.Errorf("filesystem")
	}
	return nil
}
