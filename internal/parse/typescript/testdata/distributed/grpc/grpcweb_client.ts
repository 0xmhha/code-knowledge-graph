// grpcweb_client.ts — W3c fixture: gRPC-web (Improbable Engineering /
// Google grpc-web) client patterns. Each pattern below emits one
// `grpc_calls` edge from the enclosing TS function to an AMBIGUOUS
// placeholder Endpoint with qname `grpc:Service.Method`.
//
// Expected placeholder Endpoint qnames (camelCase reflects the observed
// JS method name — V0 emits the AST-visible token without proto-PascalCase
// normalisation; EchoService.Echo stays PascalCase because the grpc.unary
// descriptor itself carries the PascalCase Method identifier):
//
//   grpc:UserService.getUser       (Pattern A: generated client class)
//   grpc:UserService.listUsers     (Pattern A: same client, second method)
//   grpc:EchoService.Echo          (Pattern B: grpc.unary descriptor)
//   grpc:OrderService.placeOrder   (Pattern A: pkg.XxxClient nested form)
//
// Confidence per §6.5 (c) for TS: all INFERRED (typesInfo not available
// in tree-sitter parses; we trust the import + naming convention).
// Pattern A is gated on the `@improbable-eng/grpc-web` import on line 17 —
// without it the `*Client` suffix would be too noisy (matches Redis/
// Prisma/Apollo/etc.). See module header of grpc_client.go.

import { grpc } from '@improbable-eng/grpc-web';
import { UserServiceClient } from './generated/user_service_pb_service';
import { EchoService } from './generated/echo_pb_service';
import { GetUserRequest, ListUsersRequest } from './generated/user_pb';
import { EchoRequest } from './generated/echo_pb';
import { OrderServiceClient } from './generated/order_service_pb_service';
import { PlaceOrderRequest } from './generated/order_pb';

// Pattern A: generated client class
//   const client = new XxxClient(host)
//   client.method(req, callback)
//
// Expected: grpc_calls → grpc:UserService.getUser (INFERRED)
export function callGetUser(id: string): void {
  const client = new UserServiceClient('https://api.example.com');
  const req = new GetUserRequest();
  req.setId(id);
  client.getUser(req, (err: unknown, resp: unknown) => {
    if (err) throw err;
    console.log(resp);
  });
}

// Same client instance, different method — emits a second grpc_calls
// edge sharing no placeholder with the first.
//
// Expected: grpc_calls → grpc:UserService.listUsers (INFERRED)
export function callListUsers(): void {
  const client = new UserServiceClient('https://api.example.com');
  const req = new ListUsersRequest();
  client.listUsers(req, (_err: unknown, resp: unknown) => {
    console.log(resp);
  });
}

// Pattern B: grpc.unary(Service.Method, { request, host, onEnd })
// Method service descriptor is a SelectorExpr — the service name is the
// outer identifier and the method is the trailing segment.
//
// Expected: grpc_calls → grpc:EchoService.Echo (INFERRED)
export function callEcho(message: string): void {
  const req = new EchoRequest();
  req.setMessage(message);
  grpc.unary(EchoService.Echo, {
    request: req,
    host: 'https://api.example.com',
    onEnd: (resp: unknown) => console.log(resp),
  });
}

// Pattern C: factory-shaped instantiation — `new pkg.XxxClient(host)`.
// Verifies the AST walker handles MemberExpression+NewExpression nesting.
//
// Expected: grpc_calls → grpc:OrderService.placeOrder (INFERRED)
export function callPlaceOrder(): void {
  const client = new OrderServiceClient('https://api.example.com');
  const req = new PlaceOrderRequest();
  client.placeOrder(req, (_err: unknown, resp: unknown) => {
    console.log(resp);
  });
}

// Negative case: arbitrary client-like method call on a non-gRPC object
// (e.g. localStorage, axios). Must NOT emit a grpc_calls edge.
export function notAGRPCCall(): void {
  const cache = new Map<string, string>();
  cache.get('hello');
  localStorage.getItem('token');
}
