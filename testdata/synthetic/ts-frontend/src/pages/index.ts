import { Vault } from '../contracts/Vault';
import { VaultService } from '../services/vault';

const v = new Vault();
const svc = new VaultService(v);
svc.depositFn(100);
