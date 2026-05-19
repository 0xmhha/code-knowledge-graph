// W-C W11 V6 fixture (TS half) — same-name class to exercise the
// cross-language binder (T20). The Sol Wallet contract has a
// matching TS class, so the linker should emit at least one
// binds_to edge.

export class Wallet {
  constructor(public owner: string) {}

  // Method that mirrors the Sol contract's relay surface.
  async relay(target: string, data: string): Promise<boolean> {
    return target.length > 0 && data.length > 0;
  }
}
