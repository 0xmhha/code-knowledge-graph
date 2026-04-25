import { Vault } from '../contracts/Vault';

export class VaultService {
    constructor(private vault: Vault) {}
    depositFn(amount: number) { return this.vault.deposit(amount); }
}
