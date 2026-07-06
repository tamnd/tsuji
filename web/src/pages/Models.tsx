import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { authorOf, fetchModels, formatContext, perMillion } from "../lib/api";

type Sort = "newest" | "price-low" | "price-high" | "context";

export default function Models() {
  const { data: models, isLoading } = useQuery({
    queryKey: ["models"],
    queryFn: fetchModels,
  });
  const [query, setQuery] = useState("");
  const [author, setAuthor] = useState<string | null>(null);
  const [sort, setSort] = useState<Sort>("newest");

  const authors = useMemo(() => {
    const counts = new Map<string, number>();
    for (const m of models ?? []) {
      const a = authorOf(m.id);
      counts.set(a, (counts.get(a) ?? 0) + 1);
    }
    return [...counts.entries()].sort((a, b) => b[1] - a[1]);
  }, [models]);

  const rows = useMemo(() => {
    let out = models ?? [];
    const q = query.toLowerCase().trim();
    if (q)
      out = out.filter(
        (m) =>
          m.id.toLowerCase().includes(q) ||
          m.name.toLowerCase().includes(q) ||
          m.description.toLowerCase().includes(q),
      );
    if (author) out = out.filter((m) => authorOf(m.id) === author);
    out = [...out];
    switch (sort) {
      case "newest":
        out.sort((a, b) => b.created - a.created);
        break;
      case "price-low":
        out.sort((a, b) => parseFloat(a.pricing.prompt) - parseFloat(b.pricing.prompt));
        break;
      case "price-high":
        out.sort((a, b) => parseFloat(b.pricing.prompt) - parseFloat(a.pricing.prompt));
        break;
      case "context":
        out.sort((a, b) => b.context_length - a.context_length);
        break;
    }
    return out;
  }, [models, query, author, sort]);

  return (
    <div className="mx-auto flex max-w-6xl gap-8 px-4 py-10">
      <aside className="hidden w-52 shrink-0 lg:block">
        <div className="mb-3 text-xs font-medium uppercase tracking-wide text-dim">
          Authors
        </div>
        <ul className="space-y-1 text-sm">
          <li>
            <button
              onClick={() => setAuthor(null)}
              className={`w-full rounded-lg px-2 py-1 text-left ${
                author === null ? "bg-raise text-ink" : "text-mute hover:text-ink"
              }`}
            >
              All models
            </button>
          </li>
          {authors.map(([a, n]) => (
            <li key={a}>
              <button
                onClick={() => setAuthor(a === author ? null : a)}
                className={`flex w-full justify-between rounded-lg px-2 py-1 text-left ${
                  a === author ? "bg-raise text-ink" : "text-mute hover:text-ink"
                }`}
              >
                <span className="font-mono text-xs">{a}</span>
                <span className="text-dim">{n}</span>
              </button>
            </li>
          ))}
        </ul>
      </aside>

      <div className="min-w-0 flex-1">
        <div className="mb-6 flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">Models</h1>
          <span className="text-sm text-dim">{rows.length}</span>
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter models"
            className="ml-auto w-56 rounded-lg border border-edge bg-surface px-3 py-1.5 text-sm outline-none placeholder:text-dim focus:border-accent/50"
          />
          <select
            value={sort}
            onChange={(e) => setSort(e.target.value as Sort)}
            className="rounded-lg border border-edge bg-surface px-2 py-1.5 text-sm text-mute outline-none"
          >
            <option value="newest">Newest</option>
            <option value="price-low">Price: low to high</option>
            <option value="price-high">Price: high to low</option>
            <option value="context">Context length</option>
          </select>
        </div>

        {isLoading && <div className="py-20 text-center text-dim">Loading…</div>}

        <ul className="divide-y divide-edge">
          {rows.map((m) => (
            <li key={m.id}>
              <Link
                to={`/models/${m.id}`}
                className="block rounded-lg px-3 py-4 transition-colors hover:bg-surface"
              >
                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                  <span className="font-medium">{m.name}</span>
                  <span className="font-mono text-xs text-dim">{m.id}</span>
                  <span className="ml-auto flex gap-4 font-mono text-xs text-dim">
                    <span>{formatContext(m.context_length)}</span>
                    <span>{perMillion(m.pricing.prompt)}/M in</span>
                    <span>{perMillion(m.pricing.completion)}/M out</span>
                  </span>
                </div>
                <p className="mt-1.5 line-clamp-2 max-w-3xl text-sm text-mute">
                  {m.description}
                </p>
              </Link>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
