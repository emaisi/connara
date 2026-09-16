package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	value, err := io.ReadAll(io.LimitReader(os.Stdin, 4097))
	if err != nil {
		panic(err)
	}
	password := strings.TrimRight(string(value), "\r\n")
	if len(password) < 12 || len(password) > 4096 {
		fmt.Fprintln(os.Stderr, "password must contain 12 to 4096 characters")
		os.Exit(1)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(hash))
}
