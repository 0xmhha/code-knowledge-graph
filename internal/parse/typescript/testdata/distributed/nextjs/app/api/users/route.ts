// route.ts — W1 fixture: Next.js App Router file-based routing.
//
// Path: app/api/users/route.ts → route = "/api/users".
//
// Expected Endpoint nodes (language="ts", sub_kind="http"):
//
//   http:GET /api/users
//   http:POST /api/users
//
// Expected listens_on edges: ≥ 2 (GET → endpoint, POST → endpoint).

export async function GET(req: Request): Promise<Response> {
  return new Response(JSON.stringify([{ id: 1 }]), { status: 200 });
}

export async function POST(req: Request): Promise<Response> {
  const body = await req.json();
  return new Response(JSON.stringify(body), { status: 201 });
}
