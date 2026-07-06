import { useCallback, useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import Markdown from "../components/Markdown";
import ModelPicker from "../components/ModelPicker";
import {
  buildMessages,
  deleteRoom,
  formatCost,
  getKey,
  loadRooms,
  newRoom,
  rid,
  saveRoom,
  setKey,
  streamChat,
  type Reply,
  type Room,
  type RoomParams,
} from "../lib/chat";

const DEFAULT_MODEL = "anthropic/claude-sonnet-5";
const MAX_COLUMNS = 4;

export default function Chat() {
  const [params] = useSearchParams();
  const [rooms, setRooms] = useState<Room[]>([]);
  const [roomID, setRoomID] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [key, setKeyState] = useState(getKey());
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [search, setSearch] = useState("");
  const aborts = useRef(new Map<string, AbortController>());
  const [busy, setBusy] = useState(0);

  // Load rooms once; seed the first room from ?model= when present.
  useEffect(() => {
    loadRooms().then((rs) => {
      let list = rs;
      const seed = params.get("model");
      if (list.length === 0 || seed) {
        const r = newRoom([seed || DEFAULT_MODEL]);
        list = [r, ...list];
        saveRoom(r);
      }
      setRooms(list);
      setRoomID(list[0].id);
      setLoaded(true);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const room = rooms.find((r) => r.id === roomID) ?? null;

  const update = useCallback((id: string, fn: (r: Room) => Room) => {
    setRooms((rs) =>
      rs.map((r) => {
        if (r.id !== id) return r;
        const next = fn(r);
        next.updatedAt = Date.now();
        saveRoom(next);
        return next;
      }),
    );
  }, []);

  const runReply = useCallback(
    (roomSnapshot: Room, turnID: string, model: string, turnIndex: number) => {
      const controller = new AbortController();
      aborts.current.set(`${turnID}:${model}`, controller);
      setBusy((b) => b + 1);
      const started = performance.now();

      const patch = (fn: (rep: Reply) => Reply) =>
        update(roomSnapshot.id, (r) => ({
          ...r,
          turns: r.turns.map((t) =>
            t.id === turnID ? { ...t, replies: { ...t.replies, [model]: fn(t.replies[model] ?? { content: "" }) } } : t,
          ),
        }));

      const body: Record<string, unknown> = {
        model,
        messages: buildMessages(roomSnapshot, model, turnIndex).concat([
          { role: "user", content: roomSnapshot.turns[turnIndex].prompt },
        ]),
        ...cleanParams(roomSnapshot.params),
      };

      streamChat(
        getKey(),
        body,
        {
          onContent: (text) => patch((rep) => ({ ...rep, content: rep.content + text })),
          onReasoning: (text) => patch((rep) => ({ ...rep, reasoning: (rep.reasoning ?? "") + text })),
          onFusion: (d) => patch((rep) => ({ ...rep, fusion: d })),
          onUsage: (prompt, completion, cost) =>
            patch((rep) => ({ ...rep, promptTokens: prompt, completionTokens: completion, cost })),
        },
        controller.signal,
      )
        .catch((err: Error) => {
          if (err.name !== "AbortError") patch((rep) => ({ ...rep, error: err.message }));
        })
        .finally(() => {
          aborts.current.delete(`${turnID}:${model}`);
          setBusy((b) => b - 1);
          patch((rep) => ({ ...rep, pending: false, latencyMs: Math.round(performance.now() - started) }));
        });
    },
    [update],
  );

  const send = (prompt: string) => {
    if (!room || !prompt.trim()) return;
    const turn = { id: rid(), prompt: prompt.trim(), replies: {} as Record<string, Reply> };
    for (const m of room.models) turn.replies[m] = { content: "", pending: true };
    const turnIndex = room.turns.length;
    const nextRoom: Room = {
      ...room,
      title: room.turns.length === 0 ? prompt.trim().slice(0, 40) : room.title,
      turns: [...room.turns, turn],
    };
    update(room.id, () => nextRoom);
    for (const m of room.models) runReply(nextRoom, turn.id, m, turnIndex);
  };

  const retry = (turnID: string, model: string) => {
    if (!room) return;
    const idx = room.turns.findIndex((t) => t.id === turnID);
    if (idx < 0) return;
    const nextRoom: Room = {
      ...room,
      turns: room.turns.map((t) =>
        t.id === turnID ? { ...t, replies: { ...t.replies, [model]: { content: "", pending: true } } } : t,
      ),
    };
    update(room.id, () => nextRoom);
    runReply(nextRoom, turnID, model, idx);
  };

  const stopAll = () => {
    for (const c of aborts.current.values()) c.abort();
    aborts.current.clear();
  };

  const exportRoom = () => {
    if (!room) return;
    const blob = new Blob([JSON.stringify(room, null, 2)], { type: "application/json" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `${room.title.replaceAll(/\W+/g, "-").toLowerCase() || "room"}.json`;
    a.click();
    URL.revokeObjectURL(a.href);
  };

  const importRoom = (file: File) => {
    file.text().then((text) => {
      const r = JSON.parse(text) as Room;
      r.id = rid();
      r.updatedAt = Date.now();
      saveRoom(r);
      setRooms((rs) => [r, ...rs]);
      setRoomID(r.id);
    });
  };

  if (!loaded) return <div className="py-20 text-center text-dim">Loading…</div>;

  const visibleRooms = rooms.filter(
    (r) => !search.trim() || r.title.toLowerCase().includes(search.toLowerCase()),
  );

  return (
    <div className="flex h-[calc(100vh-3.5rem)]">
      {/* Room list */}
      <aside className="hidden w-60 shrink-0 flex-col border-r border-edge lg:flex">
        <div className="flex items-center gap-2 border-b border-edge p-3">
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search rooms"
            className="min-w-0 flex-1 rounded-lg border border-edge bg-surface px-2.5 py-1.5 text-sm outline-none placeholder:text-dim"
          />
          <button
            onClick={() => {
              const r = newRoom(room?.models ?? [DEFAULT_MODEL]);
              saveRoom(r);
              setRooms((rs) => [r, ...rs]);
              setRoomID(r.id);
            }}
            className="rounded-lg border border-edge px-2.5 py-1.5 text-sm text-mute transition-colors hover:border-accent/50 hover:text-ink"
            title="New room"
          >
            +
          </button>
        </div>
        <ul className="flex-1 overflow-y-auto p-2">
          {visibleRooms.map((r) => (
            <li key={r.id} className="group flex items-center">
              <button
                onClick={() => setRoomID(r.id)}
                className={`min-w-0 flex-1 truncate rounded-lg px-2.5 py-1.5 text-left text-sm ${
                  r.id === roomID ? "bg-raise text-ink" : "text-mute hover:text-ink"
                }`}
              >
                {r.title}
              </button>
              <button
                onClick={() => {
                  deleteRoom(r.id);
                  setRooms((rs) => rs.filter((x) => x.id !== r.id));
                  if (roomID === r.id) setRoomID(rooms.find((x) => x.id !== r.id)?.id ?? null);
                }}
                className="px-1.5 text-dim opacity-0 transition-opacity hover:text-ink group-hover:opacity-100"
                title="Delete room"
              >
                ×
              </button>
            </li>
          ))}
        </ul>
      </aside>

      {/* Main area */}
      <div className="flex min-w-0 flex-1 flex-col">
        {!key && <KeyBanner onSave={(k) => (setKey(k), setKeyState(k))} />}

        {room && (
          <>
            <div className="flex items-center gap-2 border-b border-edge px-4 py-2">
              <input
                value={room.title}
                onChange={(e) => update(room.id, (r) => ({ ...r, title: e.target.value }))}
                className="min-w-0 flex-1 bg-transparent text-sm font-medium outline-none"
              />
              <RoomTotal room={room} />
              <button
                onClick={() => setSettingsOpen((v) => !v)}
                className={`rounded-lg border px-2.5 py-1 text-sm transition-colors ${
                  settingsOpen ? "border-accent/50 text-ink" : "border-edge text-mute hover:text-ink"
                }`}
              >
                Settings
              </button>
              <GearMenu onExport={exportRoom} onImport={importRoom} />
            </div>

            {settingsOpen && (
              <SettingsPanel
                room={room}
                onChange={(sys, p) => update(room.id, (r) => ({ ...r, system: sys, params: p }))}
              />
            )}

            {/* Model columns */}
            <div className="flex min-h-0 flex-1 divide-x divide-edge overflow-x-auto">
              {room.models.map((m, col) => (
                <ModelColumn
                  key={`${m}-${col}`}
                  room={room}
                  model={m}
                  onModelChange={(id) =>
                    update(room.id, (r) => ({
                      ...r,
                      models: r.models.map((x, i) => (i === col ? id : x)),
                    }))
                  }
                  onRemove={
                    room.models.length > 1
                      ? () =>
                          update(room.id, (r) => ({
                            ...r,
                            models: r.models.filter((_, i) => i !== col),
                          }))
                      : undefined
                  }
                  onRetry={(turnID) => retry(turnID, m)}
                />
              ))}
              {room.models.length < MAX_COLUMNS && (
                <div className="flex w-12 shrink-0 items-start justify-center pt-3">
                  <button
                    onClick={() =>
                      update(room.id, (r) => ({ ...r, models: [...r.models, DEFAULT_MODEL] }))
                    }
                    className="rounded-lg border border-edge px-2 py-1 text-sm text-dim transition-colors hover:border-accent/50 hover:text-ink"
                    title="Add a model column"
                  >
                    +
                  </button>
                </div>
              )}
            </div>

            <Composer disabled={!key} busy={busy > 0} onSend={send} onStop={stopAll} />
          </>
        )}
      </div>
    </div>
  );
}

function cleanParams(p: RoomParams): Record<string, number> {
  const out: Record<string, number> = {};
  for (const [k, v] of Object.entries(p)) {
    if (v !== undefined && v !== null && !Number.isNaN(v)) out[k] = v;
  }
  return out;
}

function RoomTotal({ room }: { room: Room }) {
  let cost = 0;
  for (const t of room.turns) for (const r of Object.values(t.replies)) cost += r.cost ?? 0;
  if (!cost) return null;
  return <span className="font-mono text-xs text-dim">{formatCost(cost)} total</span>;
}

function KeyBanner({ onSave }: { onSave: (k: string) => void }) {
  const [value, setValue] = useState("");
  return (
    <div className="flex flex-wrap items-center gap-3 border-b border-edge bg-surface px-4 py-3 text-sm">
      <span className="text-mute">
        Paste an API key to chat. Create one with <code className="font-mono text-xs">tsuji keys create</code>; it stays in this browser.
      </span>
      <input
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder="sk-tsuji-v1-…"
        className="w-64 rounded-lg border border-edge bg-bg px-2.5 py-1.5 font-mono text-xs outline-none placeholder:text-dim focus:border-accent/50"
      />
      <button
        onClick={() => value.trim() && onSave(value.trim())}
        className="rounded-lg bg-accent px-3 py-1.5 font-medium text-white transition-colors hover:bg-accent-soft"
      >
        Save
      </button>
    </div>
  );
}

function GearMenu({ onExport, onImport }: { onExport: () => void; onImport: (f: File) => void }) {
  const fileRef = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  return (
    <div className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className="rounded-lg border border-edge px-2.5 py-1 text-sm text-mute transition-colors hover:text-ink"
        title="Room menu"
      >
        ⋯
      </button>
      {open && (
        <div className="absolute right-0 top-full z-30 mt-1 w-44 rounded-lg border border-edge bg-surface py-1 text-sm shadow-xl">
          <button
            onClick={() => (onExport(), setOpen(false))}
            className="block w-full px-3 py-1.5 text-left text-mute hover:bg-raise hover:text-ink"
          >
            Export room as JSON
          </button>
          <button
            onClick={() => (fileRef.current?.click(), setOpen(false))}
            className="block w-full px-3 py-1.5 text-left text-mute hover:bg-raise hover:text-ink"
          >
            Import room JSON
          </button>
          <input
            ref={fileRef}
            type="file"
            accept="application/json"
            className="hidden"
            onChange={(e) => e.target.files?.[0] && onImport(e.target.files[0])}
          />
        </div>
      )}
    </div>
  );
}

const PARAM_FIELDS: { key: keyof RoomParams; label: string; step: number }[] = [
  { key: "temperature", label: "temperature", step: 0.1 },
  { key: "top_p", label: "top_p", step: 0.05 },
  { key: "top_k", label: "top_k", step: 1 },
  { key: "max_tokens", label: "max_tokens", step: 256 },
  { key: "frequency_penalty", label: "freq_penalty", step: 0.1 },
  { key: "presence_penalty", label: "pres_penalty", step: 0.1 },
  { key: "seed", label: "seed", step: 1 },
];

function SettingsPanel({
  room,
  onChange,
}: {
  room: Room;
  onChange: (system: string | undefined, params: RoomParams) => void;
}) {
  return (
    <div className="border-b border-edge bg-surface px-4 py-3">
      <textarea
        value={room.system ?? ""}
        onChange={(e) => onChange(e.target.value || undefined, room.params)}
        placeholder="System prompt for this room"
        rows={2}
        className="w-full resize-y rounded-lg border border-edge bg-bg px-3 py-2 text-sm outline-none placeholder:text-dim focus:border-accent/50"
      />
      <div className="mt-2 flex flex-wrap items-end gap-3">
        {PARAM_FIELDS.map((f) => (
          <label key={f.key} className="flex flex-col gap-1 font-mono text-[11px] text-dim">
            {f.label}
            <input
              type="number"
              step={f.step}
              value={room.params[f.key] ?? ""}
              onChange={(e) =>
                onChange(room.system, {
                  ...room.params,
                  [f.key]: e.target.value === "" ? undefined : Number(e.target.value),
                })
              }
              className="w-24 rounded border border-edge bg-bg px-2 py-1 text-xs text-ink outline-none focus:border-accent/50"
            />
          </label>
        ))}
        <button
          onClick={() => onChange(room.system, {})}
          className="rounded-lg border border-edge px-2.5 py-1 text-xs text-mute transition-colors hover:text-ink"
        >
          Reset to defaults
        </button>
      </div>
    </div>
  );
}

function ModelColumn({
  room,
  model,
  onModelChange,
  onRemove,
  onRetry,
}: {
  room: Room;
  model: string;
  onModelChange: (id: string) => void;
  onRemove?: () => void;
  onRetry: (turnID: string) => void;
}) {
  const bottomRef = useRef<HTMLDivElement>(null);
  const single = room.models.length === 1;

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: "end" });
  }, [room.turns]);

  return (
    <section className={`flex min-h-0 flex-col ${single ? "flex-1" : "w-96 shrink-0 grow"}`}>
      <div className="flex items-center gap-2 border-b border-edge px-3 py-2">
        <ModelPicker value={model} onChange={onModelChange} />
        {onRemove && (
          <button
            onClick={onRemove}
            className="ml-auto text-dim transition-colors hover:text-ink"
            title="Remove column"
          >
            ×
          </button>
        )}
      </div>
      <div className="flex-1 space-y-4 overflow-y-auto px-4 py-4">
        {room.turns.map((t) => {
          const rep = t.replies[model];
          return (
            <div key={t.id}>
              <div className="mb-2 flex justify-end">
                <div className="max-w-[85%] whitespace-pre-wrap rounded-lg bg-raise px-3 py-2 text-sm">
                  {t.prompt}
                </div>
              </div>
              {rep && <ReplyView reply={rep} onRetry={() => onRetry(t.id)} />}
            </div>
          );
        })}
        {room.turns.length === 0 && (
          <div className="pt-16 text-center text-sm text-dim">
            Send a prompt to start the conversation.
          </div>
        )}
        <div ref={bottomRef} />
      </div>
    </section>
  );
}

function ReplyView({ reply, onRetry }: { reply: Reply; onRetry: () => void }) {
  return (
    <div className="max-w-full">
      {reply.fusion && <FusionCards detail={reply.fusion} />}
      {reply.reasoning && !reply.fusion && (
        <details className="mb-2 rounded-lg border border-edge bg-surface px-3 py-2">
          <summary className="cursor-pointer text-xs text-dim">Thinking</summary>
          <div className="mt-1 whitespace-pre-wrap text-xs text-mute">{reply.reasoning}</div>
        </details>
      )}
      {reply.error ? (
        <div className="rounded-lg border border-danger-edge bg-danger-tint px-3 py-2 text-sm text-danger">
          {reply.error}
        </div>
      ) : reply.content ? (
        <Markdown text={reply.content} />
      ) : reply.pending ? (
        <div className="text-sm text-dim">…</div>
      ) : null}
      <div className="mt-1.5 flex flex-wrap gap-3 font-mono text-[11px] text-dim">
        {reply.promptTokens !== undefined && (
          <span>
            {reply.promptTokens} in / {reply.completionTokens} out
          </span>
        )}
        {reply.cost !== undefined && <span>{formatCost(reply.cost)}</span>}
        {reply.latencyMs !== undefined && !reply.pending && <span>{(reply.latencyMs / 1000).toFixed(1)}s</span>}
        {!reply.pending && (
          <button onClick={onRetry} className="text-dim underline-offset-2 hover:text-ink hover:underline">
            retry
          </button>
        )}
      </div>
    </div>
  );
}

// FusionCards renders the fusion extension block: collapsible panel
// answers, judge notes, and the per-phase cost breakdown.
export function FusionCards({ detail }: { detail: import("../lib/chat").FusionDetail }) {
  return (
    <div className="mb-3 space-y-1.5">
      {detail.panel.map((p) => (
        <details key={p.model} className="rounded-lg border border-edge bg-surface px-3 py-1.5">
          <summary className="flex cursor-pointer items-baseline gap-2 text-xs">
            <span className="font-mono text-mute">{p.model}</span>
            {p.error ? (
              <span className="text-danger">failed</span>
            ) : (
              <span className="text-dim">{formatCost(p.cost)}</span>
            )}
          </summary>
          <div className="mt-1.5 whitespace-pre-wrap text-xs text-mute">
            {p.error ?? p.content}
          </div>
        </details>
      ))}
      {detail.judge.notes && (
        <details className="rounded-lg border border-edge bg-surface px-3 py-1.5">
          <summary className="cursor-pointer text-xs text-dim">
            Judge notes <span className="font-mono">({detail.judge.model})</span>
          </summary>
          <div className="mt-1.5 whitespace-pre-wrap text-xs text-mute">{detail.judge.notes}</div>
        </details>
      )}
      <div className="font-mono text-[11px] text-dim">
        panel {formatCost(detail.panel.reduce((s, p) => s + p.cost, 0))} · judge{" "}
        {formatCost(detail.judge.cost)} · writer {formatCost(detail.writer.cost)}
      </div>
    </div>
  );
}

function Composer({
  disabled,
  busy,
  onSend,
  onStop,
}: {
  disabled: boolean;
  busy: boolean;
  onSend: (prompt: string) => void;
  onStop: () => void;
}) {
  const [value, setValue] = useState("");
  const submit = () => {
    if (disabled || busy || !value.trim()) return;
    onSend(value);
    setValue("");
  };
  return (
    <div className="border-t border-edge p-3">
      <div className="flex items-end gap-2">
        <textarea
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
          placeholder={disabled ? "Add an API key above to chat" : "Message every model in this room"}
          rows={2}
          disabled={disabled}
          className="min-h-[2.5rem] flex-1 resize-y rounded-lg border border-edge bg-surface px-3 py-2 text-sm outline-none placeholder:text-dim focus:border-accent/50 disabled:opacity-50"
        />
        {busy ? (
          <button
            onClick={onStop}
            className="rounded-lg border border-edge px-4 py-2 text-sm font-medium text-mute transition-colors hover:border-danger-edge hover:text-danger"
          >
            Stop
          </button>
        ) : (
          <button
            onClick={submit}
            disabled={disabled || !value.trim()}
            className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-accent-soft disabled:opacity-40"
          >
            Send
          </button>
        )}
      </div>
    </div>
  );
}
