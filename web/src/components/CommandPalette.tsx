import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { fetchModels, formatContext, perMillion } from "../lib/api";

export default function CommandPalette({ onClose }: { onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const navigate = useNavigate();
  const { data: models } = useQuery({ queryKey: ["models"], queryFn: fetchModels });

  useEffect(() => inputRef.current?.focus(), []);

  const hits = useMemo(() => {
    if (!models) return [];
    const q = query.toLowerCase().trim();
    const pool = q
      ? models.filter(
          (m) =>
            m.id.toLowerCase().includes(q) || m.name.toLowerCase().includes(q),
        )
      : models;
    return pool.slice(0, 8);
  }, [models, query]);

  const open = (id: string) => {
    onClose();
    navigate(`/models/${id}`);
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setCursor((c) => Math.min(c + 1, hits.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setCursor((c) => Math.max(c - 1, 0));
    } else if (e.key === "Enter" && hits[cursor]) {
      open(hits[cursor].id);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/60 pt-[15vh]"
      onClick={onClose}
    >
      <div
        className="w-full max-w-xl overflow-hidden rounded-lg border border-edge bg-surface shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setCursor(0);
          }}
          onKeyDown={onKey}
          placeholder="Search models..."
          className="w-full border-b border-edge bg-transparent px-4 py-3 text-sm outline-none placeholder:text-dim"
        />
        <ul className="max-h-80 overflow-y-auto py-1">
          {hits.map((m, i) => (
            <li key={m.id}>
              <button
                onMouseEnter={() => setCursor(i)}
                onClick={() => open(m.id)}
                className={`flex w-full items-center justify-between px-4 py-2.5 text-left text-sm ${
                  i === cursor ? "bg-raise" : ""
                }`}
              >
                <span>
                  <span className="text-ink">{m.name}</span>
                  <span className="ml-2 font-mono text-xs text-dim">{m.id}</span>
                </span>
                <span className="font-mono text-xs text-dim">
                  {formatContext(m.context_length)} · {perMillion(m.pricing.prompt)}/M
                </span>
              </button>
            </li>
          ))}
          {hits.length === 0 && (
            <li className="px-4 py-6 text-center text-sm text-dim">
              No models match “{query}”
            </li>
          )}
        </ul>
      </div>
    </div>
  );
}
