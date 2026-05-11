// W-B W2 fixture — class with mixed async/sync methods.
// Expectations:
//   - `Repo.load`  Method with SubKind="async", 1 AwaitPoint (await db.query)
//   - `Repo.save`  Method with SubKind="async", 1 AwaitPoint (await db.write)
//   - `Repo.count` Method with SubKind="" (no await, no AwaitPoint)
// Total: 2 EdgeAwaits, 2 AwaitPoint nodes.

interface DB {
  query(sql: string): Promise<unknown>
  write(payload: unknown): Promise<void>
  rowCount(): number
}

export class Repo {
  constructor(private db: DB) {}

  async load(id: string): Promise<unknown> {
    return await this.db.query(`select * from t where id='${id}'`)
  }

  async save(row: unknown): Promise<void> {
    await this.db.write(row)
  }

  count(): number {
    return this.db.rowCount()
  }
}
