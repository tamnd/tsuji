import { useEffect, useState } from "react";
import { Link, NavLink, Outlet } from "react-router-dom";
import CommandPalette from "./CommandPalette";

const nav = [
  { to: "/models", label: "Models" },
  { to: "/chat", label: "Chat" },
  { to: "/rankings", label: "Rankings" },
  { to: "/docs", label: "Docs" },
];

export default function Shell() {
  const [paletteOpen, setPaletteOpen] = useState(false);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setPaletteOpen((v) => !v);
      }
      if (e.key === "Escape") setPaletteOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="min-h-screen flex flex-col">
      <header className="sticky top-0 z-40 border-b border-edge bg-bg/90 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-6 px-4">
          <Link to="/" className="flex items-center gap-2 font-semibold tracking-tight">
            <span className="text-accent-soft text-lg leading-none">辻</span>
            <span>tsuji</span>
          </Link>
          <button
            onClick={() => setPaletteOpen(true)}
            className="hidden items-center gap-2 rounded-lg border border-edge bg-surface px-3 py-1.5 text-sm text-dim transition-colors hover:border-accent/50 hover:text-mute sm:flex"
          >
            <span>Search models</span>
            <kbd className="rounded border border-edge px-1 font-mono text-[10px] text-dim">
              ⌘K
            </kbd>
          </button>
          <nav className="ml-auto flex items-center gap-1 text-sm">
            {nav.map((n) => (
              <NavLink
                key={n.to}
                to={n.to}
                className={({ isActive }) =>
                  `rounded-lg px-3 py-1.5 transition-colors ${
                    isActive ? "bg-raise text-ink" : "text-mute hover:text-ink"
                  }`
                }
              >
                {n.label}
              </NavLink>
            ))}
            <button className="ml-2 rounded-lg bg-accent px-3 py-1.5 font-medium text-white transition-colors hover:bg-accent-soft">
              Sign in
            </button>
          </nav>
        </div>
      </header>

      <main className="flex-1">
        <Outlet />
      </main>

      <footer className="border-t border-edge">
        <div className="mx-auto grid max-w-6xl grid-cols-2 gap-8 px-4 py-12 text-sm sm:grid-cols-4">
          <div>
            <div className="mb-3 flex items-center gap-2 font-semibold">
              <span className="text-accent-soft">辻</span> tsuji
            </div>
            <p className="text-dim">
              A self-hostable gateway and marketplace for LLMs. One API, every
              model.
            </p>
          </div>
          <FooterCol
            title="Product"
            links={[
              ["Models", "/models"],
              ["Chat", "/chat"],
              ["Rankings", "/rankings"],
              ["Fusion", "/fusion"],
            ]}
          />
          <FooterCol
            title="Developers"
            links={[
              ["Docs", "/docs"],
              ["Quickstart", "/docs/quickstart"],
              ["API reference", "/docs/api"],
              ["Status", "/status"],
            ]}
          />
          <FooterCol
            title="Project"
            links={[
              ["GitHub", "https://github.com/tamnd/tsuji"],
              ["Privacy", "/privacy"],
              ["Terms", "/terms"],
            ]}
          />
        </div>
      </footer>

      {paletteOpen && <CommandPalette onClose={() => setPaletteOpen(false)} />}
    </div>
  );
}

function FooterCol({
  title,
  links,
}: {
  title: string;
  links: [string, string][];
}) {
  return (
    <div>
      <div className="mb-3 font-medium text-mute">{title}</div>
      <ul className="space-y-2">
        {links.map(([label, to]) => (
          <li key={label}>
            {to.startsWith("http") ? (
              <a href={to} className="text-dim transition-colors hover:text-ink">
                {label}
              </a>
            ) : (
              <Link to={to} className="text-dim transition-colors hover:text-ink">
                {label}
              </Link>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
