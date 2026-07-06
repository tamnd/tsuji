import { useState, type ComponentProps } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import "highlight.js/styles/github-dark-dimmed.css";

// Markdown renders assistant output: GFM tables, highlighted code blocks
// with a copy button. Code blocks stay dark in both themes to match the
// highlight palette.
export default function Markdown({ text }: { text: string }) {
  return (
    <div className="chat-md text-sm leading-relaxed">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{ pre: Pre }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}

function Pre(props: ComponentProps<"pre">) {
  const [copied, setCopied] = useState(false);
  const copy = (e: React.MouseEvent<HTMLButtonElement>) => {
    const pre = e.currentTarget.parentElement?.querySelector("pre");
    if (!pre) return;
    navigator.clipboard.writeText(pre.textContent ?? "");
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <div className="group relative my-3">
      <button
        onClick={copy}
        className="absolute right-2 top-2 rounded border border-[#444c56] bg-[#2d333b] px-2 py-0.5 text-[11px] text-[#909dab] opacity-0 transition-opacity hover:text-[#cdd9e5] group-hover:opacity-100"
      >
        {copied ? "Copied" : "Copy"}
      </button>
      <pre
        {...props}
        className="overflow-x-auto rounded-lg bg-[#22272e] p-3 font-mono text-[12.5px] leading-relaxed"
      />
    </div>
  );
}
