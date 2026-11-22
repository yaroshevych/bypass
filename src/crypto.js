import { Kdbx, CryptoEngine, Credentials, ProtectedValue } from "kdbxweb";
import { argon2d, argon2id } from "hash-wasm";
class UnsupportedArgon2 extends Error {}
const argon2ByType = new Map([[0, argon2d], [2, argon2id]]);
async function deriveArgon2(password, salt, memory, iterations, length, parallelism, type, version) {
  const hash = argon2ByType.get(type);
  if (!hash || version !== 0x13) throw new UnsupportedArgon2();
  const result = await hash({ password: new Uint8Array(password), salt: new Uint8Array(salt), memorySize: memory, iterations, hashLength: length, parallelism, outputType: "binary" });
  return result.buffer;
}
CryptoEngine.setArgon2Impl(deriveArgon2);
export { Kdbx, Credentials, ProtectedValue, UnsupportedArgon2 };
