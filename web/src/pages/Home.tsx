import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { fetchModels, formatContext, perMillion } from "../lib/api";

const snippet = `import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "http://localhost:4780/api/v1",
  apiKey: "sk-tsuji-v1-...",
});

const done = await client.chat.completions.create({
  model: "anthropic/claude-sonnet-5",
  messages: [{ role: "user", content: "Hello" }],
});`;

export default function Home() {
  const { data: models } = useQuery({ queryKey: ["models"], queryFn: fetchModels });
  const featured = (models ?? []).slice(0, 6);

  return (
    <div className="mx-auto max-w-6xl px-4">
      <section className="grid items-center gap-10 py-20 lg:grid-cols-2">
        <div>
          <h1 className="text-4xl font-semibold tracking-tight sm:text-5xl">
            One API,
            <br />
            <span className="text-accent-soft">every model.</span>
          </h1>
          <p className="mt-5 max-w-md text-lg text-mute">
            Route your requests to the best model for the job. Automatic
            fallbacks, honest prices, and an OpenAI-compatible API you already
            know how to use.
          </p>
          <div className="mt-8 flex gap-3">
            <Link
              to="/models"
              className="rounded-lg bg-accent px-4 py-2 font-medium text-white transition-colors hover:bg-accent-soft"
            >
              Browse models
            </Link>
            <Link
              to="/docs"
              className="rounded-lg border border-edge px-4 py-2 font-medium text-mute transition-colors hover:border-accent/50 hover:text-ink"
            >
              Read the docs
            </Link>
          </div>
        </div>
        <pre className="overflow-x-auto rounded-lg border border-edge bg-surface p-5 font-mono text-[13px] leading-relaxed text-mute">
          <code>{snippet}</code>
        </pre>
      </section>

      <section className="border-t border-edge py-16">
        <div className="mb-8 flex items-end justify-between">
          <h2 className="text-2xl font-semibold tracking-tight">
            Featured models
          </h2>
          <Link to="/models" className="text-sm text-accent-soft hover:underline">
            View all →
          </Link>
        </div>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {featured.map((m) => (
            <Link
              key={m.id}
              to={`/models/${m.id}`}
              className="rounded-lg border border-edge bg-surface p-5 transition-colors hover:border-accent/50"
            >
              <div className="font-medium">{m.name}</div>
              <div className="mt-1 font-mono text-xs text-dim">{m.id}</div>
              <p className="mt-3 line-clamp-2 text-sm text-mute">
                {m.description}
              </p>
              <div className="mt-4 flex gap-4 font-mono text-xs text-dim">
                <span>{formatContext(m.context_length)} context</span>
                <span>{perMillion(m.pricing.prompt)}/M in</span>
                <span>{perMillion(m.pricing.completion)}/M out</span>
              </div>
            </Link>
          ))}
        </div>
      </section>

      <section className="grid gap-px overflow-hidden rounded-lg border border-edge bg-edge sm:grid-cols-3">
        {[
          ["Automatic fallbacks", "When a provider degrades, requests walk to the next endpoint before your users notice."],
          ["Price-aware routing", "Requests favor the cheapest healthy endpoint by default. Pin providers when you need to."],
          ["Bring your own keys", "Run it on your hardware with your provider keys. Your prompts never leave your control."],
        ].map(([title, body]) => (
          <div key={title} className="bg-surface p-6">
            <div className="font-medium">{title}</div>
            <p className="mt-2 text-sm text-mute">{body}</p>
          </div>
        ))}
      </section>
      <div className="h-20" />
    </div>
  );
}
