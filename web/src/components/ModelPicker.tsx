import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { fetchModels } from "../lib/api";

// ModelPicker is the searchable combobox used per chat column.
export default function ModelPicker({
  value,
  onChange,
}: {
  value: string;
  onChange: (id: string) => void;
}) {
  const { data: models } = useQuery({ queryKey: ["models"], queryFn: fetchModels });
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState(0);
  const ref = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    inputRef.current?.focus();
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  const hits = useMemo(() => {
    const q = query.toLowerCase().trim();
    let out = models ?? [];
    if (q) out = out.filter((m) => m.id.toLowerCase().includes(q) || m.name.toLowerCase().includes(q));
    return out.slice(0, 10);
  }, [models, query]);

  const pick = (id: string) => {
    onChange(id);
    setOpen(false);
    setQuery("");
  };

  return (
    <div ref={ref} className="relative min-w-0">
      <button
        onClick={() => setOpen((v) => !v)}
        className="max-w-full truncate rounded-lg border border-edge bg-surface px-2.5 py-1 font-mono text-xs text-mute transition-colors hover:border-accent/50 hover:text-ink"
        title={value}
      >
        {value || "Pick a model"}
      </button>
      {open && (
        <div className="absolute left-0 top-full z-30 mt-1 w-72 rounded-lg border border-edge bg-surface shadow-xl">
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setCursor(0);
            }}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") setCursor((c) => Math.min(c + 1, hits.length - 1));
              if (e.key === "ArrowUp") setCursor((c) => Math.max(c - 1, 0));
              if (e.key === "Enter" && hits[cursor]) pick(hits[cursor].id);
              if (e.key === "Escape") setOpen(false);
            }}
            placeholder="Search models"
            className="w-full border-b border-edge bg-transparent px-3 py-2 text-sm outline-none placeholder:text-dim"
          />
          <ul className="max-h-64 overflow-y-auto py-1">
            {hits.map((m, i) => (
              <li key={m.id}>
                <button
                  onClick={() => pick(m.id)}
                  onMouseEnter={() => setCursor(i)}
                  className={`w-full px-3 py-1.5 text-left ${i === cursor ? "bg-raise" : ""}`}
                >
                  <div className="truncate text-sm">{m.name}</div>
                  <div className="truncate font-mono text-[11px] text-dim">{m.id}</div>
                </button>
              </li>
            ))}
            {hits.length === 0 && <li className="px-3 py-2 text-sm text-dim">No matches</li>}
          </ul>
        </div>
      )}
    </div>
  );
}
