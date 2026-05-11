// fastify.ts — W1 fixture: Fastify route registration patterns.
//
// Expected Endpoint nodes (language="ts", sub_kind="http"):
//
//   http:GET /ping
//   http:POST /items
//   http:PUT /items/:id
//
// Expected listens_on edges: ≥ 3.

import Fastify from 'fastify';

const fastify = Fastify();

function pingHandler(req: any, reply: any): void {
  reply.send({ pong: true });
}

fastify.get('/ping', pingHandler);

fastify.post('/items', async function createItem(req: any, reply: any) {
  return { ok: true };
});

// fastify.route({ method, url, handler }) — object-literal form.
fastify.route({
  method: 'PUT',
  url: '/items/:id',
  handler: async function updateItem(req: any, reply: any) {
    return { ok: true };
  },
});
