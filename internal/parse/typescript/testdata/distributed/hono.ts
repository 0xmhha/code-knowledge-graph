// hono.ts — W1 fixture: Hono fluent API endpoint detection.
//
// Expected Endpoint nodes (language="ts", sub_kind="http"):
//
//   http:GET /api/hello
//   http:POST /api/hello
//   http:GET /api/users/:id
//
// Expected listens_on edges: ≥ 3 (inline arrow handlers).

import { Hono } from 'hono';

const app = new Hono();

function helloHandler(c: any) {
  return c.json({ hello: 'world' });
}

app.get('/api/hello', helloHandler);

app.post('/api/hello', (c: any) => c.json({ created: true }));

app.get('/api/users/:id', async function userById(c: any) {
  return c.json({ id: c.req.param('id') });
});

export default app;
