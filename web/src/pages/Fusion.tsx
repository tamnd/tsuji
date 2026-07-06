import { useState } from "react";
import { Link } from "react-router-dom";
import { getKey, streamChat, type FusionDetail } from "../lib/chat";
import { formatCost } from "../lib/chat";
import Markdown from "../components/Markdown";
import { FusionCards } from "./Chat";

const snippet = `const done = await client.chat.completions.create({
  model: "tsuji/fusion",
  messages: [{ role: "user", content: "Should we shard this table?" }],
});

// done.fusion holds the panel answers, judge notes,
// and the per-phase cost breakdown.`;

const steps: [string, string, string][] = [
  [
    "1. Panel",
    "Your prompt fans out to several models at once. Each answers independently, without seeing the others.",
    "A member that errors just drops out; the run continues as long as two answers survive.",
  ],
  [
    "2. Judge",
    "A judge model reads every panel answer and writes comparison notes: where the panel agrees, where it contradicts itself, and which claims look unreliable.",
    "The notes stream to you as reasoning, so you see the analysis before the answer.",
  ],
  [
    "3. Writer",
    "A writer model composes the final answer from the panel plus the judge notes, keeping the consensus and dropping what was flagged.",
    "You are billed the sum of every leg, itemized per phase.",
  ],
];

const tiers: [string, string, string][] = [
  ["tsuji/fusion", "general-high", "Frontier panel. The strongest answer money can buy."],
  ["tsuji/fusion-budget", "budget", "Inexpensive panel. Close to one frontier call in total cost."],
  ["tsuji/fusion-fast", "fast", "Small, low-latency panel. The writer starts sooner."],
];

export default function Fusion() {
  return (
    <div className="mx-auto max-w-6xl px-4">
      <section className="py-20 text-center">
        <div className="mb-3 font-mono text-sm text-accent-soft">tsuji/fusion</div>
        <h1 className="mx-auto max-w-2xl text-4xl font-semibold tracking-tight sm:text-5xl">
          Several models answer. One answer wins.
        </h1>
        <p className="mx-auto mt-5 max-w-xl text-lg text-mute">
          Fusion runs your prompt past a panel of models, has a judge compare
          their answers, and has a writer compose the best final response.
          One request, one response, the whole panel behind it.
        </p>
        <div className="mt-8 flex justify-center gap-3">
          <Link
            to="/chat?model=tsuji/fusion"
            className="rounded-lg bg-accent px-4 py-2 font-medium text-white transition-colors hover:bg-accent-soft"
          >
            Try it in Chat
          </Link>
          <Link
            to="/models/tsuji/fusion"
            className="rounded-lg border border-edge px-4 py-2 font-medium text-mute transition-colors hover:border-accent/50 hover:text-ink"
          >
            Model page
          </Link>
        </div>
      </section>

      <section className="grid gap-px overflow-hidden rounded-lg border border-edge bg-edge sm:grid-cols-3">
        {steps.map(([title, body, note]) => (
          <div key={title} className="bg-surface p-6">
            <div className="font-medium">{title}</div>
            <p className="mt-2 text-sm text-mute">{body}</p>
            <p className="mt-2 text-sm text-dim">{note}</p>
          </div>
        ))}
      </section>

      <section className="grid items-start gap-10 py-16 lg:grid-cols-2">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">
            An OpenAI call like any other
          </h2>
          <p className="mt-3 max-w-md text-mute">
            Pick a fusion model id and everything else stays the same: same
            endpoint, same SDK, streaming included. The response carries a
            fusion block with every panel answer, the judge notes, and what
            each phase cost.
          </p>
          <ul className="mt-6 space-y-3 text-sm text-mute">
            {tiers.map(([id, preset, blurb]) => (
              <li key={id} className="flex flex-wrap items-baseline gap-x-3">
                <Link to={`/models/${id}`} className="font-mono text-accent-soft hover:underline">
                  {id}
                </Link>
                <span className="font-mono text-xs text-dim">{preset}</span>
                <span className="w-full text-dim sm:w-auto sm:flex-1">{blurb}</span>
              </li>
            ))}
          </ul>
        </div>
        <pre className="overflow-x-auto rounded-lg border border-edge bg-surface p-5 font-mono text-[13px] leading-relaxed text-mute">
          <code>{snippet}</code>
        </pre>
      </section>

      <Demo />
      <div className="h-20" />
    </div>
  );
}

// Demo is a live box wired to tsuji/fusion through the same SSE path the
// playground uses. It needs a playground key; without one it points at /chat.
function Demo() {
  const [prompt, setPrompt] = useState("");
  const [content, setContent] = useState("");
  const [reasoning, setReasoning] = useState("");
  const [detail, setDetail] = useState<FusionDetail | null>(null);
  const [cost, setCost] = useState<number | undefined>();
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");
  const key = getKey();

  const run = () => {
    if (!prompt.trim() || running) return;
    setContent("");
    setReasoning("");
    setDetail(null);
    setCost(undefined);
    setError("");
    setRunning(true);
    streamChat(
      key,
      { model: "tsuji/fusion", messages: [{ role: "user", content: prompt.trim() }] },
      {
        onContent: (t) => setContent((c) => c + t),
        onReasoning: (t) => setReasoning((r) => r + t),
        onFusion: setDetail,
        onUsage: (_p, _c, cost) => setCost(cost),
      },
      new AbortController().signal,
    )
      .catch((err: Error) => setError(err.message))
      .finally(() => setRunning(false));
  };

  return (
    <section className="border-t border-edge py-16">
      <h2 className="text-2xl font-semibold tracking-tight">See a run</h2>
      {key ? (
        <div className="mt-6 rounded-lg border border-edge bg-surface p-5">
          <div className="flex gap-2">
            <input
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && run()}
              placeholder="Ask the panel something"
              className="flex-1 rounded-lg border border-edge bg-bg px-3 py-2 text-sm outline-none placeholder:text-dim focus:border-accent/50"
            />
            <button
              onClick={run}
              disabled={running || !prompt.trim()}
              className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-accent-soft disabled:opacity-40"
            >
              {running ? "Running…" : "Run"}
            </button>
          </div>
          {error && <div className="mt-4 text-sm text-danger">{error}</div>}
          {reasoning && (
            <details className="mt-4 rounded-lg border border-edge bg-bg px-3 py-2" open={!content}>
              <summary className="cursor-pointer text-xs text-dim">Judge notes</summary>
              <div className="mt-1 whitespace-pre-wrap text-xs text-mute">{reasoning}</div>
            </details>
          )}
          {content && (
            <div className="mt-4">
              <Markdown text={content} />
            </div>
          )}
          {detail && (
            <div className="mt-4">
              <FusionCards detail={detail} />
            </div>
          )}
          {cost !== undefined && (
            <div className="mt-2 font-mono text-xs text-dim">total {formatCost(cost)}</div>
          )}
        </div>
      ) : (
        <p className="mt-4 text-mute">
          The live demo uses your playground key. Add one in{" "}
          <Link to="/chat" className="text-accent-soft hover:underline">
            Chat
          </Link>{" "}
          and come back, or just open fusion there directly.
        </p>
      )}
    </section>
  );
}
