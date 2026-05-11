// connectweb_client.ts — W3c fixture: Connect-web (Buf Connect /
// @bufbuild/connect-web) client patterns. Each pattern below emits one
// `grpc_calls` edge from the enclosing TS function to an AMBIGUOUS
// placeholder Endpoint with qname `grpc:Service.Method`.
//
// Expected placeholder Endpoint qnames (camelCase reflects the observed
// JS method name — V0 emits the AST-visible token without proto-PascalCase
// normalisation):
//
//   grpc:GreetService.sayHello         (createPromiseClient + method call)
//   grpc:GreetService.sayGoodbye       (await client.sayGoodbye(req))
//   grpc:NotificationService.subscribe (createClient — Connect-ES new shape)
//
// Confidence per §6.5 (c) for TS: all INFERRED. Patterns B/C
// (createPromiseClient / createClient) are NOT gated on imports — the
// factory names are distinctive enough on their own.

import { createPromiseClient } from '@bufbuild/connect-web';
import { createClient } from '@connectrpc/connect';
import { createConnectTransport } from '@bufbuild/connect-web';
import { GreetService } from './gen/greet_connectweb';
import { NotificationService } from './gen/notification_connect';
import { SayHelloRequest, SayGoodbyeRequest } from './gen/greet_pb';
import { SubscribeRequest } from './gen/notification_pb';

// Pattern A: createPromiseClient(Service, transport) — Buf Connect-web V0.
// Multiple method calls on the same client emit one grpc_calls edge each,
// both sharing the same Service-derived qname prefix.
//
// Expected: grpc_calls → grpc:GreetService.sayHello (INFERRED)
// Expected: grpc_calls → grpc:GreetService.sayGoodbye (INFERRED)
export async function callGreet(name: string): Promise<void> {
  const transport = createConnectTransport({
    baseUrl: 'https://api.example.com',
  });
  const client = createPromiseClient(GreetService, transport);
  const req = new SayHelloRequest({ name });
  const resp = await client.sayHello(req);
  console.log(resp);

  const goodbye = new SayGoodbyeRequest({ name });
  await client.sayGoodbye(goodbye);
}

// Pattern B: createClient(Service, transport) — Connect-ES rename.
// Equivalent emit semantics.
//
// Expected: grpc_calls → grpc:NotificationService.subscribe (INFERRED)
export async function callSubscribe(): Promise<void> {
  const transport = createConnectTransport({
    baseUrl: 'https://api.example.com',
  });
  const client = createClient(NotificationService, transport);
  const req = new SubscribeRequest();
  await client.subscribe(req);
}

// Negative case: createPromiseClient-like name but on an unknown wrapper
// type. The first arg isn't a service descriptor; we still treat it as
// INFERRED because the Connect convention is to pass the service ident
// here. Documented as a known false-positive risk for V0.
//
// (No explicit no-emit assertion — this exists to round out the fixture.)
export function ambiguousFactory(): void {
  // Plain function call without a service ident — must not emit anything.
  const noop = (): void => undefined;
  noop();
}
