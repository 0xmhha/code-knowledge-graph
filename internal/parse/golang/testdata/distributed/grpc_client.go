// Package distributed_fixture (grpc client) — W3b of schema 1.9 (CKS G5
// Distributed cross-language interop): Go gRPC client call patterns.
//
// Expected emit (per call site below, all under the enclosing function):
//
//   - CallGetUser            → grpc_calls → grpc:UserService.GetUser
//     (same-file server registered this Endpoint,
//     so the client edge resolves to the real
//     language="go" Endpoint, not a placeholder)
//   - CallListUsers          → grpc_calls → grpc:UserService.ListUsers
//     (same as GetUser — real Endpoint reuse)
//   - CallEcho               → grpc_calls → grpc:EchoService.Echo
//     (real Endpoint, same-file registration)
//   - CallExternalService    → grpc_calls → grpc:ExternalService.DoSomething
//     (no server registration in fixture →
//     AMBIGUOUS placeholder Endpoint retained)
//   - NonGRPCCallSkipped     → no edge (variable is not a gRPC stub)
package distributed_fixture

import (
	"context"

	pb "ckgdistributed.test/grpcstubs"
)

// CallGetUser exercises the stub-var bookkeeping path: `client` is bound
// to pb.NewUserServiceClient(conn), then client.GetUser(ctx, req) emits
// a grpc_calls edge to grpc:UserService.GetUser.
func CallGetUser(conn *pb.Conn) {
	client := pb.NewUserServiceClient(conn)
	_, _ = client.GetUser(context.Background(), &pb.GetUserRequest{})
}

// CallListUsers exercises a second method on the same stub var — both
// edges should resolve via the variable-name map.
func CallListUsers(conn *pb.Conn) {
	client := pb.NewUserServiceClient(conn)
	_, _ = client.ListUsers(context.Background(), &pb.ListUsersRequest{})
}

// CallEcho exercises the EchoService client — distinct service, distinct
// stub-var binding. Confirms the per-function grpcClientStubs reset (a
// stub named `client` here is EchoServiceClient, not UserServiceClient).
func CallEcho(conn *pb.Conn) {
	client := pb.NewEchoServiceClient(conn)
	_, _ = client.Echo(context.Background(), &pb.EchoRequest{})
}

// CallExternalService targets a service that no server-side handler in
// this fixture registers. After the parse, the grpc_calls edge points at
// an AMBIGUOUS placeholder Endpoint (language="external") — surfacing the
// external service call for audit.
func CallExternalService(conn *pb.Conn) {
	client := pb.NewExternalServiceClient(conn)
	_, _ = client.DoSomething(context.Background(), &pb.DoSomethingRequest{})
}

// NonGRPCCallSkipped is a non-gRPC method invocation. The receiver `s` is
// a string, and even though `Trim` matches the exported-method shape, the
// stub-var map binding is absent → no grpc_calls edge. Documented
// false-positive guard.
func NonGRPCCallSkipped() {
	s := "  hello  "
	_ = s
	// Intentionally no gRPC call here.
}

// CallGetUserVarForm exercises the `var name = ...` stub-binding shape
// (vs the `:=` form used above). Both shapes must populate
// grpcClientStubs, so this function emits the same kind of edge as
// CallGetUser.
func CallGetUserVarForm(conn *pb.Conn) {
	var client = pb.NewUserServiceClient(conn)
	_, _ = client.GetUser(context.Background(), &pb.GetUserRequest{})
}
