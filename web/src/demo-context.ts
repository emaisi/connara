import { createContext } from "react";
import type { DemoContextValue } from "./demo";
export const DemoContext = createContext<DemoContextValue | null>(null);
