package gents

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sharedRuntimeOrders = `package orders

message Order { id: string }
message Missing @status(404) { reason: string }

service Orders {
  base_path: "/v1"
  get(Order) -> Order | Missing @get("/orders/{id}")
  watch(Order) -> Order @get("/orders/{id}/watch") @stream
}
`

const sharedRuntimeUsers = `package users

message User { id: string }

service Users {
  base_path: "/v1"
  get(User) -> User @get("/users/{id}")
}
`

func writeSharedRuntimeProject(t *testing.T, forNode bool) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"onekitrt", "orders", "users"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runtimeDir := filepath.Join(dir, "onekitrt")
	writeFile(t, filepath.Join(runtimeDir, "runtime.ts"), string(GenerateClientRuntime()))
	for pkg, schema := range map[string]string{"orders": sharedRuntimeOrders, "users": sharedRuntimeUsers} {
		file := compileTSSchema(t, schema)
		opts := Options{SharedRuntime: "../onekitrt/runtime.js"}
		types := string(GenerateTypesWithOptions(file, nil, opts))
		client := string(GenerateClientWithOptions(file, nil, opts))
		if forNode {
			types = strings.ReplaceAll(types, `.js"`, `.ts"`)
			client = strings.ReplaceAll(client, `.js"`, `.ts"`)
		}
		writeFile(t, filepath.Join(dir, pkg, "types.ts"), types)
		writeFile(t, filepath.Join(dir, pkg, "client.ts"), client)
	}
	return dir
}

func TestSharedClientRuntimeMakesApiErrorOneClassAcrossPackages(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	dir := writeSharedRuntimeProject(t, true)
	writeFile(t, filepath.Join(dir, "main.ts"), `
import { ApiError, TypedApiError, RequestValidationError } from "./onekitrt/runtime.ts";
import { OrdersClient, ApiError as OrdersApiError, RequestValidationError as OrdersValidation } from "./orders/client.ts";
import { UsersClient, ApiError as UsersApiError, TypedApiError as UsersTypedApiError } from "./users/client.ts";

const failing = (status: number, body: unknown) => (async () => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } })) as unknown as typeof fetch;

if (OrdersApiError !== ApiError || UsersApiError !== ApiError) throw new Error("every client module must re-export the one shared ApiError");
if (UsersTypedApiError !== TypedApiError || OrdersValidation !== RequestValidationError) throw new Error("the other runtime classes are shared too");

const orders = new OrdersClient("http://x", { fetch: failing(500, { error: { code: "boom", message: "orders broke", request_id: "r1" } }) });
const users = new UsersClient("http://x", { fetch: failing(503, { error: { code: "down", message: "users down", request_id: "r2" } }) });

const caught: unknown[] = [];
for (const call of [() => orders.get({ id: "1" }), () => users.get({ id: "2" })]) {
  try { await call(); } catch (error) { caught.push(error); }
}
if (caught.length !== 2) throw new Error("both calls must fail");
for (const error of caught) {
  if (!(error instanceof ApiError)) throw new Error("one instanceof ApiError must match errors from every package");
}
const [first, second] = caught as ApiError[];
if (first.statusCode !== 500 || first.code !== "boom" || first.requestId !== "r1") throw new Error("orders error: " + JSON.stringify(first));
if (second.statusCode !== 503 || second.code !== "down" || second.requestId !== "r2") throw new Error("users error: " + JSON.stringify(second));

let validation: unknown;
try { await users.get({ id: 1 as unknown as string }); } catch (error) { validation = error; }
if (!(validation instanceof RequestValidationError)) throw new Error("validation errors are shared as well: " + String(validation));
console.log("OK");
`)
	cmd := exec.Command("node", "main.ts")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "OK" {
		t.Fatalf("node run failed: %v\n%s", err, out)
	}
}

func TestSharedClientRuntimeTypeChecksUnderStrictUnusedFlags(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc not available")
	}
	dir := writeSharedRuntimeProject(t, false)
	writeFile(t, filepath.Join(dir, "tsconfig.json"), `{
  "compilerOptions": {
    "target": "ES2022", "module": "ES2022", "moduleResolution": "bundler",
    "strict": true, "noUnusedLocals": true, "noUnusedParameters": true, "noEmit": true,
    "lib": ["ES2022", "DOM"], "types": []
  }
}
`)
	cmd := exec.Command("tsc", "-p", "tsconfig.json")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s", err, out)
	}
	for _, pkg := range []string{"orders", "users"} {
		data, err := os.ReadFile(filepath.Join(dir, pkg, "client.ts"))
		if err != nil {
			t.Fatal(err)
		}
		client := string(data)
		if strings.Contains(client, "export class ApiError") || strings.Contains(client, "function requestSignal") {
			t.Errorf("%s/client.ts must not declare its own runtime", pkg)
		}
	}
}
