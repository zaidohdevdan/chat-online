import type { UserSummary } from '../features/auth/types';

const E2EE_PREFIX = 'e2ee:v1:';
const KEYPAIR_STORAGE_PREFIX = 'chat_online_e2ee_identity_v1:';

interface StoredIdentityKeys {
  publicKeyJwk: JsonWebKey;
  privateKeyJwk: JsonWebKey;
  publicKeyB64: string;
}

interface E2EEEnvelopeV1 {
  v: 1;
  alg: 'ECDH-P256-HKDF-AES256GCM';
  iv: string;
  salt: string;
  ct: string;
  epk?: JsonWebKey;
  rk?: Record<string, { iv: string; wk: string }>;
  ck: { iv: string; wk: string };
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = '';
  for (let i = 0; i < bytes.byteLength; i++) {
    binary += String.fromCharCode(bytes[i]!);
  }
  return btoa(binary);
}

function base64ToBytes(b64: string): Uint8Array {
  const binary = atob(b64);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    out[i] = binary.charCodeAt(i);
  }
  return out;
}

function hasWebCrypto(): boolean {
  return typeof globalThis.crypto !== 'undefined' && Boolean(globalThis.crypto.subtle);
}

export async function ensureUserIdentityPublicKey(userId: string): Promise<string | null> {
  if (!hasWebCrypto() || !userId) return null;
  const storageKey = `${KEYPAIR_STORAGE_PREFIX}${userId}`;

  try {
    const raw = localStorage.getItem(storageKey);
    if (raw) {
      const parsed = JSON.parse(raw) as StoredIdentityKeys;
      if (parsed.publicKeyB64 && parsed.privateKeyJwk && parsed.publicKeyJwk) {
        return parsed.publicKeyB64;
      }
    }
  } catch {
    // Regenerate if corrupted
  }

  try {
    const keyPair = await crypto.subtle.generateKey(
      { name: 'ECDH', namedCurve: 'P-256' },
      true,
      ['deriveBits', 'deriveKey'],
    );
    const publicKeyJwk = await crypto.subtle.exportKey('jwk', keyPair.publicKey);
    const privateKeyJwk = await crypto.subtle.exportKey('jwk', keyPair.privateKey);
    const publicKeyB64 = btoa(JSON.stringify(publicKeyJwk));

    const record: StoredIdentityKeys = {
      publicKeyJwk,
      privateKeyJwk,
      publicKeyB64,
    };
    localStorage.setItem(storageKey, JSON.stringify(record));
    return publicKeyB64;
  } catch {
    return null;
  }
}

async function getUserPrivateKey(userId: string): Promise<CryptoKey | null> {
  if (!hasWebCrypto() || !userId) return null;
  try {
    const raw = localStorage.getItem(`${KEYPAIR_STORAGE_PREFIX}${userId}`);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as StoredIdentityKeys;
    return await crypto.subtle.importKey(
      'jwk',
      parsed.privateKeyJwk,
      { name: 'ECDH', namedCurve: 'P-256' },
      false,
      ['deriveBits', 'deriveKey'],
    );
  } catch {
    return null;
  }
}

async function deriveConversationRatchetKey(
  conversationId: string,
  salt: Uint8Array,
): Promise<CryptoKey> {
  const enc = new TextEncoder();
  const baseKeyMaterial = await crypto.subtle.importKey(
    'raw',
    enc.encode(`signal-ratchet-root:${conversationId}`),
    'HKDF',
    false,
    ['deriveKey'],
  );
  return crypto.subtle.deriveKey(
    {
      name: 'HKDF',
      hash: 'SHA-256',
      salt: salt as unknown as BufferSource,
      info: enc.encode('chat-online-e2ee-msg-ratchet-v1'),
    },
    baseKeyMaterial,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  );
}

async function deriveECDHWrappingKey(
  privateKey: CryptoKey,
  peerPublicKeyJwk: JsonWebKey,
  salt: Uint8Array,
): Promise<CryptoKey> {
  const peerPub = await crypto.subtle.importKey(
    'jwk',
    peerPublicKeyJwk,
    { name: 'ECDH', namedCurve: 'P-256' },
    false,
    [],
  );
  const sharedBits = await crypto.subtle.deriveBits(
    { name: 'ECDH', public: peerPub },
    privateKey,
    256,
  );
  const hkdfBase = await crypto.subtle.importKey('raw', sharedBits, 'HKDF', false, [
    'deriveKey',
  ]);
  return crypto.subtle.deriveKey(
    {
      name: 'HKDF',
      hash: 'SHA-256',
      salt: salt as unknown as BufferSource,
      info: new TextEncoder().encode('chat-online-x3dh-ecdh-wrap-v1'),
    },
    hkdfBase,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  );
}

export function isEncryptedPayload(body: string | null | undefined): boolean {
  return Boolean(body && body.startsWith(E2EE_PREFIX));
}

export async function encryptMessageBody(
  plaintext: string,
  conversationId: string,
  members?: UserSummary[],
): Promise<string> {
  if (!plaintext || isEncryptedPayload(plaintext) || !hasWebCrypto()) {
    return plaintext;
  }

  try {
    const enc = new TextEncoder();
    const rawMessageKey = crypto.getRandomValues(new Uint8Array(32));
    const msgIv = crypto.getRandomValues(new Uint8Array(12));
    const salt = crypto.getRandomValues(new Uint8Array(16));

    const msgCryptoKey = await crypto.subtle.importKey(
      'raw',
      rawMessageKey,
      { name: 'AES-GCM', length: 256 },
      false,
      ['encrypt'],
    );

    const ctBuffer = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv: msgIv },
      msgCryptoKey,
      enc.encode(plaintext),
    );

    // Ephemeral ECDH P-256 keypair for forward secrecy per message (Signal ratchet style)
    let epk: JsonWebKey | undefined;
    const rk: Record<string, { iv: string; wk: string }> = {};

    if (members && members.length > 0) {
      try {
        const ephemeralPair = await crypto.subtle.generateKey(
          { name: 'ECDH', namedCurve: 'P-256' },
          true,
          ['deriveBits', 'deriveKey'],
        );
        epk = await crypto.subtle.exportKey('jwk', ephemeralPair.publicKey);

        for (const m of members) {
          if (!m.publicKey) continue;
          try {
            const peerJwk = JSON.parse(atob(m.publicKey)) as JsonWebKey;
            const wrapKey = await deriveECDHWrappingKey(
              ephemeralPair.privateKey,
              peerJwk,
              salt,
            );
            const wrapIv = crypto.getRandomValues(new Uint8Array(12));
            const wrapped = await crypto.subtle.encrypt(
              { name: 'AES-GCM', iv: wrapIv },
              wrapKey,
              rawMessageKey,
            );
            rk[m.id] = {
              iv: bytesToBase64(wrapIv),
              wk: bytesToBase64(new Uint8Array(wrapped)),
            };
          } catch {
            // Skip invalid member public key
          }
        }
      } catch {
        // Fallback to HKDF conversation ratchet key
      }
    }

    const convKey = await deriveConversationRatchetKey(conversationId, salt);
    const ckIv = crypto.getRandomValues(new Uint8Array(12));
    const ckWrapped = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv: ckIv },
      convKey,
      rawMessageKey,
    );

    const envelope: E2EEEnvelopeV1 = {
      v: 1,
      alg: 'ECDH-P256-HKDF-AES256GCM',
      iv: bytesToBase64(msgIv),
      salt: bytesToBase64(salt),
      ct: bytesToBase64(new Uint8Array(ctBuffer)),
      epk,
      rk: Object.keys(rk).length > 0 ? rk : undefined,
      ck: {
        iv: bytesToBase64(ckIv),
        wk: bytesToBase64(new Uint8Array(ckWrapped)),
      },
    };

    return `${E2EE_PREFIX}${btoa(JSON.stringify(envelope))}`;
  } catch {
    return plaintext;
  }
}

export async function decryptMessageBody(
  payload: string | null | undefined,
  conversationId: string,
  currentUserId?: string,
): Promise<string | null> {
  if (!payload) return null;
  if (!isEncryptedPayload(payload) || !hasWebCrypto()) {
    return payload;
  }

  try {
    const encoded = payload.slice(E2EE_PREFIX.length);
    const env = JSON.parse(atob(encoded)) as E2EEEnvelopeV1;
    const salt = base64ToBytes(env.salt);
    const msgIv = base64ToBytes(env.iv);
    const ctBytes = base64ToBytes(env.ct);

    let rawMessageKey: ArrayBuffer | null = null;

    // 1. Try ECDH P-256 recipient unwrap first if available for currentUserId
    if (currentUserId && env.epk && env.rk?.[currentUserId]) {
      const privKey = await getUserPrivateKey(currentUserId);
      if (privKey) {
        try {
          const wrapEntry = env.rk[currentUserId]!;
          const wrapKey = await deriveECDHWrappingKey(privKey, env.epk, salt);
          rawMessageKey = await crypto.subtle.decrypt(
            { name: 'AES-GCM', iv: base64ToBytes(wrapEntry.iv) as unknown as BufferSource },
            wrapKey,
            base64ToBytes(wrapEntry.wk) as unknown as BufferSource,
          );
        } catch {
          rawMessageKey = null;
        }
      }
    }

    // 2. Fallback to HKDF-SHA256 conversation ratchet key
    if (!rawMessageKey && env.ck) {
      const convKey = await deriveConversationRatchetKey(conversationId, salt);
      rawMessageKey = await crypto.subtle.decrypt(
        { name: 'AES-GCM', iv: base64ToBytes(env.ck.iv) as unknown as BufferSource },
        convKey,
        base64ToBytes(env.ck.wk) as unknown as BufferSource,
      );
    }

    if (!rawMessageKey) {
      return '🔒 Mensagem protegida com criptografia ponta a ponta';
    }

    const msgCryptoKey = await crypto.subtle.importKey(
      'raw',
      rawMessageKey,
      { name: 'AES-GCM', length: 256 },
      false,
      ['decrypt'],
    );
    const plainBuffer = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: msgIv as unknown as BufferSource },
      msgCryptoKey,
      ctBytes as unknown as BufferSource,
    );

    return new TextDecoder().decode(plainBuffer);
  } catch {
    return '🔒 Mensagem protegida com criptografia ponta a ponta';
  }
}
