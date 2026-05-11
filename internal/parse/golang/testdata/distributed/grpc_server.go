// Package distributed_fixture (grpc server) — W3b of schema 1.9 (CKS G5
// Distributed cross-language interop): Go gRPC server registration patterns.
// The fixture exercises the AST-only path of maybeEmitGRPCServerRegistration
// — typesInfo is unavailable because testdata/distributed has no real
// `pb` import resolving to a generated protobuf package (the import below
// resolves to an empty stub file, see grpc_stubs.go).
//
// Expected emit (one grpc_listens_on edge per method on the impl receiver):
//
//   - userServiceImpl.GetUser    → grpc_listens_on → grpc:UserService.GetUser
//   - userServiceImpl.ListUsers  → grpc_listens_on → grpc:UserService.ListUsers
//   - echoServiceImpl.Echo       → grpc_listens_on → grpc:EchoService.Echo
//
// Methods named with lowercase first letter (unexported) MUST NOT emit
// edges — gRPC RPCs are always exported. The `helperMethod` below verifies
// this guard.
package distributed_fixture

import (
	"context"

	pb "ckgdistributed.test/grpcstubs"
)

// userServiceImpl is the concrete server-side implementation registered via
// pb.RegisterUserServiceServer in StartUserServer below. The two methods
// (GetUser, ListUsers) become Endpoint nodes `grpc:UserService.GetUser` and
// `grpc:UserService.ListUsers` after parsing.
type userServiceImpl struct {
	pb.UnimplementedUserServiceServer
}

// GetUser is the GetUser RPC handler. Exported → emits grpc_listens_on.
func (u *userServiceImpl) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.GetUserResponse, error) {
	return &pb.GetUserResponse{}, nil
}

// ListUsers is the ListUsers RPC handler. Exported → emits grpc_listens_on.
func (u *userServiceImpl) ListUsers(ctx context.Context, req *pb.ListUsersRequest) (*pb.ListUsersResponse, error) {
	return &pb.ListUsersResponse{}, nil
}

// helperMethod is unexported — MUST NOT emit a grpc_listens_on edge even
// though it's a method on the registered impl type. False-positive guard.
func (u *userServiceImpl) helperMethod() string {
	return "internal"
}

// StartUserServer registers the UserService impl with the gRPC server.
// pb.RegisterUserServiceServer(s, &userServiceImpl{}) triggers the W3b
// server-side detector. The AST shape used for the impl arg is
// `&userServiceImpl{}` (UnaryExpr{Op:&, X:CompositeLit{Type:Ident}}),
// covered by grpcImplTypeName.
func StartUserServer(s *pb.Server) {
	pb.RegisterUserServiceServer(s, &userServiceImpl{})
}

// echoServiceImpl is a second registered service in the same file so
// the per-(file, service) dedup map in grpcServerImpls is exercised.
type echoServiceImpl struct{}

// Echo is the only RPC on EchoService. Exported → emits grpc_listens_on.
func (e *echoServiceImpl) Echo(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	return &pb.EchoResponse{}, nil
}

// StartEchoServer registers EchoService with the same server. Mixing two
// Register calls in one file confirms the grpcServerImpls dedup keys on
// service name (not just file path).
func StartEchoServer(s *pb.Server) {
	pb.RegisterEchoServiceServer(s, &echoServiceImpl{})
}
