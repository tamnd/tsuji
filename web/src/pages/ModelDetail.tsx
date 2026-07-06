import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  fetchModelEndpoints,
  fetchModels,
  formatContext,
  perMillion,
} from "../lib/api";

export default function ModelDetail() {
  const { author = "", slug = "" } = useParams();
  const id = `${author}/${slug}`;

  const { data: models } = useQuery({ queryKey: ["models"], queryFn: fetchModels });
  const { data: detail, isLoading } = useQuery({
    queryKey: ["endpoints", id],
    queryFn: () => fetchModelEndpoints(author, slug),
  });

  const model = models?.find((m) => m.id === id);

  if (isLoading)
    return <div className="py-24 text-center text-dim">Loading…</div>;
  if (!detail)
    return (
      <div className="py-24 text-center">
        <div className="text-lg">Model not found</div>
        <Link to="/models" className="mt-2 inline-block text-sm text-accent-soft hover:underline">
          ← Back to models
        </Link>
      </div>
    );

  return (
    <div className="mx-auto max-w-6xl px-4 py-10">
      <nav className="mb-6 text-sm text-dim">
        <Link to="/models" className="hover:text-ink">
          Models
        </Link>{" "}
        / <span className="font-mono">{id}</span>
      </nav>

      <div className="flex flex-wrap items-start gap-4">
        <div className="min-w-0">
          <h1 className="text-3xl font-semibold tracking-tight">
            {detail.name}
          </h1>
          <div className="mt-1 flex items-center gap-3">
            <code className="rounded bg-surface px-2 py-0.5 font-mono text-xs text-mute">
              {id}
            </code>
            <span className="text-xs text-dim">
              Added {new Date(detail.created * 1000).toLocaleDateString()}
            </span>
          </div>
        </div>
        <Link
          to={`/chat?model=${encodeURIComponent(id)}`}
          className="ml-auto rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-accent-soft"
        >
          Chat
        </Link>
      </div>

      <p className="mt-6 max-w-3xl text-mute">{detail.description}</p>

      {model && (
        <div className="mt-8 grid gap-px overflow-hidden rounded-lg border border-edge bg-edge sm:grid-cols-4">
          <Stat label="Context" value={formatContext(model.context_length)} />
          <Stat
            label="Max output"
            value={
              model.top_provider.max_completion_tokens
                ? formatContext(model.top_provider.max_completion_tokens)
                : "—"
            }
          />
          <Stat label="Input" value={`${perMillion(model.pricing.prompt)}/M`} />
          <Stat
            label="Output"
            value={`${perMillion(model.pricing.completion)}/M`}
          />
        </div>
      )}

      {model && (
        <div className="mt-6 flex flex-wrap gap-2">
          {model.architecture.input_modalities.map((m) => (
            <span
              key={m}
              className="rounded-full border border-edge px-2.5 py-0.5 text-xs text-mute"
            >
              {m} in
            </span>
          ))}
          {model.supported_parameters.slice(0, 10).map((p) => (
            <span
              key={p}
              className="rounded-full border border-edge px-2.5 py-0.5 font-mono text-xs text-dim"
            >
              {p}
            </span>
          ))}
        </div>
      )}

      <h2 className="mt-12 text-xl font-semibold tracking-tight">
        Providers for {detail.name}
      </h2>
      <p className="mt-1 text-sm text-dim">
        tsuji routes across these endpoints, cheapest healthy first.
      </p>
      <div className="mt-4 overflow-x-auto rounded-lg border border-edge">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-edge bg-surface text-left text-xs text-dim">
              <th className="px-4 py-2.5 font-medium">Provider</th>
              <th className="px-4 py-2.5 font-medium">Context</th>
              <th className="px-4 py-2.5 font-medium">Max output</th>
              <th className="px-4 py-2.5 font-medium">Quantization</th>
              <th className="px-4 py-2.5 font-medium">Input $/M</th>
              <th className="px-4 py-2.5 font-medium">Output $/M</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-edge">
            {detail.endpoints.map((e) => (
              <tr key={e.name} className="hover:bg-surface">
                <td className="px-4 py-3 font-medium">{e.provider_name}</td>
                <td className="px-4 py-3 font-mono text-xs text-mute">
                  {formatContext(e.context_length)}
                </td>
                <td className="px-4 py-3 font-mono text-xs text-mute">
                  {e.max_completion_tokens
                    ? formatContext(e.max_completion_tokens)
                    : "—"}
                </td>
                <td className="px-4 py-3 font-mono text-xs text-mute">
                  {e.quantization ?? "—"}
                </td>
                <td className="px-4 py-3 font-mono text-xs text-mute">
                  {perMillion(e.pricing.prompt)}
                </td>
                <td className="px-4 py-3 font-mono text-xs text-mute">
                  {perMillion(e.pricing.completion)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-surface px-4 py-3">
      <div className="text-xs text-dim">{label}</div>
      <div className="mt-0.5 font-mono text-sm">{value}</div>
    </div>
  );
}
