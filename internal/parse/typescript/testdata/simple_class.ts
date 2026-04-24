import { Vault } from './vault';

export class WalletService {
    private vault: Vault;
    constructor(v: Vault) { this.vault = v; }
    deposit(amount: number): void {
        this.vault.deposit(amount);
    }
}
