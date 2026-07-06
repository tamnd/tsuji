import { Link } from "react-router-dom";

// Catch-all for routes that do not exist yet.
export default function NotFound() {
  return (
    <div className="mx-auto max-w-6xl px-4 py-32 text-center">
      <div className="font-mono text-sm text-dim">404</div>
      <h1 className="mt-2 text-3xl font-semibold tracking-tight">
        Nothing at this crossroads
      </h1>
      <p className="mx-auto mt-3 max-w-md text-mute">
        This page does not exist, or is not built yet.
      </p>
      <div className="mt-8 flex justify-center gap-3">
        <Link
          to="/"
          className="rounded-lg bg-accent px-4 py-2 font-medium text-white transition-colors hover:bg-accent-soft"
        >
          Go home
        </Link>
        <Link
          to="/models"
          className="rounded-lg border border-edge px-4 py-2 font-medium text-mute transition-colors hover:border-accent/50 hover:text-ink"
        >
          Browse models
        </Link>
      </div>
    </div>
  );
}
