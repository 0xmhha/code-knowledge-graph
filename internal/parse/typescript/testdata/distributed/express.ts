// express.ts — W1 fixture: Express + Router HTTP server endpoint detection.
//
// Expected nodes (Endpoint, language="ts", sub_kind="http"):
//
//   http:GET /users
//   http:POST /users
//   http:GET /admin
//   http:DELETE /admin/:id
//   http:PATCH /computed  (NOTE: route is a variable — INFERRED confidence)
//
// Expected listens_on edges: ≥ 4 (one per named handler / inline lambda).
// Duplicate route declarations dedup to a single Endpoint node.

import express, { Router } from 'express';

const app = express();
const router = Router();

function getUsers(req: any, res: any): void {
  res.json([{ id: 1 }]);
}

function postUser(req: any, res: any): void {
  res.status(201).json({});
}

app.get('/users', getUsers);
app.post('/users', postUser);

// Duplicate declaration of the same route — must dedup.
app.get('/users', getUsers);

router.get('/admin', function adminHandler(req: any, res: any): void {
  res.send('admin');
});

router.delete('/admin/:id', (req: any, res: any) => {
  res.status(204).end();
});

// Computed route — emitted as INFERRED with "<computed>" label.
const dynRoute = '/dynamic';
app.patch(dynRoute, (req: any, res: any) => {
  res.send('dyn');
});
