import { lazy, StrictMode, Suspense } from "react";
import { createRoot } from "react-dom/client";
import { createBrowserRouter, RouterProvider } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "./index.css";
import Shell from "./components/Shell";
import Home from "./pages/Home";
import Models from "./pages/Models";
import ModelDetail from "./pages/ModelDetail";
import NotFound from "./pages/NotFound";

// The chat pages pull in markdown + highlighting; load them on demand.
const Chat = lazy(() => import("./pages/Chat"));
const Fusion = lazy(() => import("./pages/Fusion"));

const fallback = <div className="py-20 text-center text-dim">Loading…</div>;

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 60_000, retry: 1 } },
});

const router = createBrowserRouter([
  {
    element: <Shell />,
    children: [
      { path: "/", element: <Home /> },
      { path: "/models", element: <Models /> },
      { path: "/models/:author/:slug", element: <ModelDetail /> },
      { path: "/chat", element: <Suspense fallback={fallback}><Chat /></Suspense> },
      { path: "/fusion", element: <Suspense fallback={fallback}><Fusion /></Suspense> },
      { path: "*", element: <NotFound /> },
    ],
  },
]);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
