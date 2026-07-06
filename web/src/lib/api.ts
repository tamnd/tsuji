export type Model = {
  id: string;
  name: string;
  created: number;
  description: string;
  context_length: number;
  architecture: {
    modality: string;
    input_modalities: string[];
    output_modalities: string[];
    tokenizer: string;
  };
  pricing: {
    prompt: string;
    completion: string;
    request: string;
    image: string;
  };
  top_provider: {
    context_length: number;
    max_completion_tokens: number | null;
    is_moderated: boolean;
  };
  supported_parameters: string[];
};

export type ModelEndpoint = {
  name: string;
  provider_name: string;
  context_length: number;
  max_completion_tokens: number | null;
  quantization: string | null;
  pricing: { prompt: string; completion: string };
  supported_parameters: string[];
};

export type ModelEndpoints = {
  id: string;
  name: string;
  created: number;
  description: string;
  endpoints: ModelEndpoint[];
};

export type Provider = {
  name: string;
  slug: string;
  privacy_policy_url: string | null;
  terms_of_service_url: string | null;
  status_page_url: string | null;
};

async function get<T>(path: string): Promise<T> {
  const res = await fetch(path);
  if (!res.ok) throw new Error(`${path}: ${res.status}`);
  return res.json();
}

export const fetchModels = () =>
  get<{ data: Model[] }>("/api/v1/models").then((r) => r.data);

export const fetchModelEndpoints = (author: string, slug: string) =>
  get<{ data: ModelEndpoints }>(
    `/api/v1/models/${author}/${slug}/endpoints`,
  ).then((r) => r.data);

export const fetchProviders = () =>
  get<{ data: Provider[] }>("/api/v1/providers").then((r) => r.data);

// perMillion turns a per-token dollar string into a $/M display value.
export function perMillion(perToken: string): string {
  const n = parseFloat(perToken);
  if (!n) return "$0";
  const m = n * 1_000_000;
  const digits = m >= 10 ? 0 : m >= 1 ? 2 : 3;
  return `$${m.toFixed(digits)}`;
}

export function formatContext(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n % 1_000_000 ? 2 : 0)}M`;
  if (n >= 1_000) return `${Math.round(n / 1_000)}K`;
  return String(n);
}

export function authorOf(id: string): string {
  return id.split("/")[0];
}
