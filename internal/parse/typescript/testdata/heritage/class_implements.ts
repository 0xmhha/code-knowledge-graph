// W-B W1 fixture — class implements multiple interfaces (same-file).
// Expectations: 2 EdgeImplements (Service → IService / ILogger), ConfExtracted.
// No EdgeExtends.

interface IService {
  start(): void
}

interface ILogger {
  log(msg: string): void
}

class Service implements IService, ILogger {
  start(): void {}
  log(msg: string): void {
    console.log(msg)
  }
}
