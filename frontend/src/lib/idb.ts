/**
 * Minimal IndexedDB wrapper for storing CryptoKey objects.
 *
 * IndexedDB is the only browser storage that natively accepts non-extractable
 * CryptoKey values. localStorage only stores strings, so it forces exportable
 * JWK — which means any XSS can read the raw key bytes.
 *
 * This module exposes a thin get/set/delete/clearAll API over a single
 * object store ('keys') in the 'chat_online_crypto_v1' database.
 */

const DB_NAME = 'chat_online_crypto_v1';
const DB_VERSION = 1;
const STORE_KEYS = 'keys';

function openCryptoDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      if (!req.result.objectStoreNames.contains(STORE_KEYS)) {
        req.result.createObjectStore(STORE_KEYS);
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
    req.onblocked = () => reject(new Error('IndexedDB open blocked'));
  });
}

export async function idbGet<T>(key: string): Promise<T | undefined> {
  const db = await openCryptoDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_KEYS, 'readonly');
    const req = tx.objectStore(STORE_KEYS).get(key);
    req.onsuccess = () => resolve(req.result as T | undefined);
    req.onerror = () => reject(req.error);
    tx.oncomplete = () => db.close();
  });
}

export async function idbSet(key: string, value: unknown): Promise<void> {
  const db = await openCryptoDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_KEYS, 'readwrite');
    tx.objectStore(STORE_KEYS).put(value, key);
    tx.oncomplete = () => {
      db.close();
      resolve();
    };
    tx.onerror = () => {
      db.close();
      reject(tx.error);
    };
  });
}

export async function idbDelete(key: string): Promise<void> {
  const db = await openCryptoDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_KEYS, 'readwrite');
    tx.objectStore(STORE_KEYS).delete(key);
    tx.oncomplete = () => {
      db.close();
      resolve();
    };
    tx.onerror = () => {
      db.close();
      reject(tx.error);
    };
  });
}

/** Wipes all stored crypto keys — call on logout. */
export async function idbClearAll(): Promise<void> {
  const db = await openCryptoDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_KEYS, 'readwrite');
    tx.objectStore(STORE_KEYS).clear();
    tx.oncomplete = () => {
      db.close();
      resolve();
    };
    tx.onerror = () => {
      db.close();
      reject(tx.error);
    };
  });
}
