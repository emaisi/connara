import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { queryClient } from "./query";
afterEach(() => queryClient.clear());
