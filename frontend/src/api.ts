// The server API. A 401 tells the app to show the sign-in screen again.
export class HTTPError extends Error {
  constructor(
    message: string,
    public status: number,
    public data: Record<string, unknown>,
  ) {
    super(message);
  }
}

async function check(res: Response, path: string): Promise<Response> {
  if (res.ok) return res;
  if (res.status === 401 && path !== "login")
    window.dispatchEvent(new Event("parlor-unauthorized"));
  const data = await res.json().catch(() => ({ error: "Request failed" }));
  throw new HTTPError(data.error || "Request failed", res.status, data);
}

export async function api<T>(path: string, body?: unknown, method?: string): Promise<T> {
  const res = await fetch("/api/" + path, {
    method: method ?? (body === undefined ? "GET" : "POST"),
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return (await check(res, path)).json();
}

// upload sends a file as multipart form data, field "file".
export async function upload<T>(path: string, file: File, fields: Record<string, string> = {}) {
  const form = new FormData();
  form.append("file", file);
  for (const [k, v] of Object.entries(fields)) form.append(k, v);
  const res = await fetch("/api/" + path, { method: "POST", body: form });
  return (await check(res, path)).json() as Promise<T>;
}

// bytes fetches a binary response, or null on 404.
export async function bytes(path: string): Promise<{ data: Uint8Array; id: number } | null> {
  const res = await fetch("/api/" + path);
  if (res.status === 404) return null;
  await check(res, path);
  return {
    data: new Uint8Array(await res.arrayBuffer()),
    id: Number(res.headers.get("X-Save-Id") ?? 0),
  };
}

// putBytes sends raw bytes with PUT.
export async function putBytes<T>(path: string, data: Uint8Array): Promise<T> {
  const res = await fetch("/api/" + path, {
    method: "PUT",
    headers: { "Content-Type": "application/octet-stream" },
    body: data as BodyInit,
  });
  return (await check(res, path)).json();
}
