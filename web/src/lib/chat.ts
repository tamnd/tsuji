// Chat playground state: rooms live entirely in the browser (IndexedDB),
// requests go through the real /api/v1/chat/completions SSE path.

export type FusionPanelEntry = {
  model: string;
  content?: string;
  error?: string;
  cost: number;
};

export type FusionDetail = {
  preset: string;
  panel: FusionPanelEntry[];
  judge: { model: string; notes?: string; cost: number };
  writer: { model: string; cost: number };
};

export type Reply = {
  content: string;
  reasoning?: string;
  fusion?: FusionDetail;
  promptTokens?: number;
  completionTokens?: number;
  cost?: number;
  latencyMs?: number;
  error?: string;
  pending?: boolean;
};

// A turn is one user prompt plus one reply per model column.
export type Turn = {
  id: string;
  prompt: string;
  replies: Record<string, Reply>;
};

export type RoomParams = {
  temperature?: number;
  top_p?: number;
  top_k?: number;
  max_tokens?: number;
  frequency_penalty?: number;
  presence_penalty?: number;
  seed?: number;
};

export type Room = {
  id: string;
  title: string;
  models: string[];
  system?: string;
  params: RoomParams;
  turns: Turn[];
  createdAt: number;
  updatedAt: number;
};

export function newRoom(models: string[]): Room {
  const now = Date.now();
  return {
    id: rid(),
    title: "New room",
    models,
    params: {},
    turns: [],
    createdAt: now,
    updatedAt: now,
  };
}

export function rid(): string {
  return Math.random().toString(36).slice(2, 10) + now36();
}

function now36(): string {
  return Date.now().toString(36);
}

// --- IndexedDB persistence ---------------------------------------------

const DB_NAME = "tsuji-chat";
const STORE = "rooms";

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, 1);
    req.onupgradeneeded = () => {
      req.result.createObjectStore(STORE, { keyPath: "id" });
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

function tx<T>(mode: IDBTransactionMode, run: (s: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  return openDB().then(
    (db) =>
      new Promise<T>((resolve, reject) => {
        const t = db.transaction(STORE, mode);
        const req = run(t.objectStore(STORE));
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
        t.oncomplete = () => db.close();
      }),
  );
}

export const loadRooms = () =>
  tx<Room[]>("readonly", (s) => s.getAll() as IDBRequest<Room[]>).then((rooms) =>
    rooms.sort((a, b) => b.updatedAt - a.updatedAt),
  );

export const saveRoom = (room: Room) => tx("readwrite", (s) => s.put(room));

export const deleteRoom = (id: string) => tx("readwrite", (s) => s.delete(id));

// --- API key ------------------------------------------------------------

const KEY_STORAGE = "tsuji-playground-key";

export const getKey = () => localStorage.getItem(KEY_STORAGE) ?? "";
export const setKey = (k: string) => {
  if (k) localStorage.setItem(KEY_STORAGE, k);
  else localStorage.removeItem(KEY_STORAGE);
};

// --- Streaming ----------------------------------------------------------

type WireDelta = {
  content?: string | null;
  reasoning?: string | null;
};

type WireChunk = {
  choices?: { delta?: WireDelta; finish_reason?: string | null }[];
  usage?: {
    prompt_tokens: number;
    completion_tokens: number;
    cost?: number;
  } | null;
  fusion?: FusionDetail;
  error?: { message: string };
};

export type StreamHandlers = {
  onContent: (text: string) => void;
  onReasoning: (text: string) => void;
  onFusion: (d: FusionDetail) => void;
  onUsage: (prompt: number, completion: number, cost?: number) => void;
};

// streamChat runs one model reply over SSE. Resolves when the stream ends;
// rejects on transport or gateway errors. Abort via the signal.
export async function streamChat(
  key: string,
  body: Record<string, unknown>,
  handlers: StreamHandlers,
  signal: AbortSignal,
): Promise<void> {
  const res = await fetch("/api/v1/chat/completions", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${key}`,
    },
    body: JSON.stringify({ ...body, stream: true }),
    signal,
  });
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try {
      const e = await res.json();
      if (e?.error?.message) msg = e.error.message;
    } catch {
      // keep the status line
    }
    throw new Error(msg);
  }
  if (!res.body) throw new Error("no response body");

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let idx;
    while ((idx = buf.indexOf("\n\n")) >= 0) {
      const frame = buf.slice(0, idx);
      buf = buf.slice(idx + 2);
      for (const line of frame.split("\n")) {
        if (!line.startsWith("data: ")) continue;
        const data = line.slice(6);
        if (data === "[DONE]") return;
        let chunk: WireChunk;
        try {
          chunk = JSON.parse(data);
        } catch {
          continue;
        }
        if (chunk.error) throw new Error(chunk.error.message);
        if (chunk.fusion) handlers.onFusion(chunk.fusion);
        if (chunk.usage) {
          handlers.onUsage(
            chunk.usage.prompt_tokens,
            chunk.usage.completion_tokens,
            chunk.usage.cost ?? undefined,
          );
        }
        for (const c of chunk.choices ?? []) {
          if (c.delta?.content) handlers.onContent(c.delta.content);
          if (c.delta?.reasoning) handlers.onReasoning(c.delta.reasoning);
        }
      }
    }
  }
}

// buildMessages flattens a room's history into the wire format for one
// model column: shared user prompts plus that model's own past replies.
export function buildMessages(room: Room, model: string, upTo?: number): { role: string; content: string }[] {
  const msgs: { role: string; content: string }[] = [];
  if (room.system?.trim()) msgs.push({ role: "system", content: room.system });
  const end = upTo ?? room.turns.length;
  for (let i = 0; i < end; i++) {
    const t = room.turns[i];
    msgs.push({ role: "user", content: t.prompt });
    const r = t.replies[model];
    if (r && !r.error && r.content) msgs.push({ role: "assistant", content: r.content });
  }
  return msgs;
}

export function formatCost(cost?: number): string {
  if (cost === undefined) return "";
  if (cost === 0) return "$0";
  // Two significant digits, never scientific notation: $0.000088, $0.0012, $0.35.
  const digits = Math.max(2, 1 - Math.floor(Math.log10(cost)));
  return `$${cost.toFixed(Math.min(digits, 10))}`;
}
