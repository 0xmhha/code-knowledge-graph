// Package grpcstubs is a hand-written stand-in for the .pb.go files that
// protoc-gen-go would generate from grpc.proto. We hand-roll just enough
// surface to make grpc_server.go and grpc_client.go type-check under
// `packages.Load` so the W3b detector's typesInfo path (EXTRACTED
// confidence) gets exercised in the test fixture.
//
// Naming follows the standard `protoc-gen-go-grpc` convention:
//   - Register<Svc>Server(reg, impl)
//   - <Svc>Server interface (the impl satisfies this)
//   - Unimplemented<Svc>Server embed for forward compatibility
//   - <Svc>Client interface (the stub returned by New<Svc>Client)
//   - New<Svc>Client(conn) *<svc>Client
//
// The bodies are no-ops — only the shapes matter.
package grpcstubs

import "context"

// Server is a stand-in for *grpc.Server. The fixture's Register* calls
// take this type as the first arg; we only need a named placeholder.
type Server struct{}

// Conn is a stand-in for *grpc.ClientConn. New<Svc>Client takes this.
type Conn struct{}

// ── UserService ───────────────────────────────────────────────────────────

type GetUserRequest struct{ UserID string }
type GetUserResponse struct{ Name string }
type ListUsersRequest struct{ PageSize int32 }
type ListUsersResponse struct{ Users []*GetUserResponse }

type UserServiceServer interface {
	GetUser(context.Context, *GetUserRequest) (*GetUserResponse, error)
	ListUsers(context.Context, *ListUsersRequest) (*ListUsersResponse, error)
}

// UnimplementedUserServiceServer is the standard forward-compat embed.
type UnimplementedUserServiceServer struct{}

func (UnimplementedUserServiceServer) mustEmbedUnimplementedUserServiceServer() {}

func RegisterUserServiceServer(s *Server, srv UserServiceServer) {
	_ = s
	_ = srv
}

type UserServiceClient interface {
	GetUser(context.Context, *GetUserRequest) (*GetUserResponse, error)
	ListUsers(context.Context, *ListUsersRequest) (*ListUsersResponse, error)
}

type userServiceClient struct{}

func (userServiceClient) GetUser(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
	return &GetUserResponse{}, nil
}

func (userServiceClient) ListUsers(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error) {
	return &ListUsersResponse{}, nil
}

func NewUserServiceClient(c *Conn) UserServiceClient {
	_ = c
	return userServiceClient{}
}

// ── EchoService ───────────────────────────────────────────────────────────

type EchoRequest struct{ Message string }
type EchoResponse struct{ Message string }

type EchoServiceServer interface {
	Echo(context.Context, *EchoRequest) (*EchoResponse, error)
}

func RegisterEchoServiceServer(s *Server, srv EchoServiceServer) {
	_ = s
	_ = srv
}

type EchoServiceClient interface {
	Echo(context.Context, *EchoRequest) (*EchoResponse, error)
}

type echoServiceClient struct{}

func (echoServiceClient) Echo(ctx context.Context, req *EchoRequest) (*EchoResponse, error) {
	return &EchoResponse{}, nil
}

func NewEchoServiceClient(c *Conn) EchoServiceClient {
	_ = c
	return echoServiceClient{}
}

// ── ExternalService ───────────────────────────────────────────────────────
// No server-side registration in the fixture — the client fixture's
// CallExternalService targets this to exercise the AMBIGUOUS placeholder
// path.

type DoSomethingRequest struct{ Input string }
type DoSomethingResponse struct{ Output string }

type ExternalServiceClient interface {
	DoSomething(context.Context, *DoSomethingRequest) (*DoSomethingResponse, error)
}

type externalServiceClient struct{}

func (externalServiceClient) DoSomething(ctx context.Context, req *DoSomethingRequest) (*DoSomethingResponse, error) {
	return &DoSomethingResponse{}, nil
}

func NewExternalServiceClient(c *Conn) ExternalServiceClient {
	_ = c
	return externalServiceClient{}
}
