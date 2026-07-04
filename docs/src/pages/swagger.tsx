import Layout from "@theme/Layout";
import BrowserOnly from "@docusaurus/BrowserOnly";
import useBaseUrl from "@docusaurus/useBaseUrl";
import { useState, useEffect, useRef, useCallback } from "react";

const STORAGE_KEY = "b4-swagger-base-url";

function buildSwaggerHtml(specUrl: string, server: string): string {
  const serverScript = server
    ? `
      const parsed = new URL(${JSON.stringify(server)});
      spec.host = parsed.host;
      spec.basePath = "/api";
      spec.schemes = [parsed.protocol.replace(":", "")];`
    : "";

  return `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8"/>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"/>
  <style>body { margin: 0; } .swagger-ui .topbar { display: none; }</style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    const API_BASE = ${JSON.stringify(server)};
    let tokenCache = { key: "", token: "" };

    async function ensureBearer(req) {
      const auth = req.headers && req.headers.Authorization;
      if (!auth) return req;
      if (/^Bearer\\s/i.test(auth)) return req;
      if (/^Basic\\s/i.test(auth)) {
        const b64 = auth.replace(/^Basic\\s+/i, "");
        let decoded = "";
        try { decoded = atob(b64); } catch (e) { return req; }
        const idx = decoded.indexOf(":");
        const username = idx >= 0 ? decoded.slice(0, idx) : decoded;
        const password = idx >= 0 ? decoded.slice(idx + 1) : "";
        if (tokenCache.key === b64 && tokenCache.token) {
          req.headers.Authorization = "Bearer " + tokenCache.token;
          return req;
        }
        if (!API_BASE) { delete req.headers.Authorization; return req; }
        try {
          const resp = await fetch(API_BASE.replace(/\\/$/, "") + "/api/auth/login", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ username, password }),
          });
          const data = await resp.json().catch(() => ({}));
          if (resp.ok && data && data.token) {
            tokenCache = { key: b64, token: data.token };
            req.headers.Authorization = "Bearer " + data.token;
          } else {
            delete req.headers.Authorization;
          }
        } catch (e) {
          delete req.headers.Authorization;
        }
        return req;
      }
      req.headers.Authorization = "Bearer " + auth;
      return req;
    }

    fetch(${JSON.stringify(specUrl)})
      .then(r => r.json())
      .then(spec => {${serverScript}
        spec.securityDefinitions = spec.securityDefinitions || {};
        spec.securityDefinitions.BasicAuth = {
          type: "basic",
          description: "Enter your b4 web UI username and password",
        };
        if (spec.paths) {
          Object.keys(spec.paths).forEach(function (p) {
            const item = spec.paths[p];
            Object.keys(item).forEach(function (m) {
              const op = item[m];
              if (op && Array.isArray(op.security)) {
                const hasBearer = op.security.some(function (s) { return s && s.BearerAuth; });
                const hasBasic = op.security.some(function (s) { return s && s.BasicAuth; });
                if (hasBearer && !hasBasic) {
                  op.security.push({ BasicAuth: [] });
                }
              }
            });
          });
        }
        SwaggerUIBundle({
          spec,
          dom_id: "#swagger-ui",
          requestInterceptor: ensureBearer,
        });
      });
  </script>
</body>
</html>`;
}

function SwaggerUILoader() {
  const latestSpecUrl = useBaseUrl("/swagger.json");
  const versionsIndexUrl = useBaseUrl("/swagger-versions/index.json");
  const versionsBaseUrl = useBaseUrl("/swagger-versions/");

  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [baseUrl, setBaseUrl] = useState(
    () => (typeof window !== "undefined" ? localStorage.getItem(STORAGE_KEY) : "") || ""
  );
  const [inputValue, setInputValue] = useState(baseUrl);
  const [versions, setVersions] = useState([]);
  const [selectedVersion, setSelectedVersion] = useState("latest");

  useEffect(() => {
    fetch(versionsIndexUrl)
      .then((r) => (r.ok ? r.json() : []))
      .then((v) => setVersions(v))
      .catch(() => {});
  }, [versionsIndexUrl]);

  const getSpecUrl = useCallback(
    (version: string) =>
      version === "latest"
        ? latestSpecUrl
        : `${versionsBaseUrl}${version}.json`,
    [latestSpecUrl, versionsBaseUrl]
  );

  const renderSwagger = useCallback(
    (specUrl: string, server: string) => {
      const iframe = iframeRef.current;
      if (!iframe) return;
      const html = buildSwaggerHtml(specUrl, server);
      iframe.srcdoc = html;
    },
    []
  );

  useEffect(() => {
    renderSwagger(getSpecUrl(selectedVersion), baseUrl);
  }, []);

  function handleConnect() {
    const url = inputValue.trim();
    setBaseUrl(url);
    if (url) {
      localStorage.setItem(STORAGE_KEY, url);
    } else {
      localStorage.removeItem(STORAGE_KEY);
    }
    renderSwagger(getSpecUrl(selectedVersion), url);
  }

  function handleDisconnect() {
    setInputValue("");
    setBaseUrl("");
    localStorage.removeItem(STORAGE_KEY);
    renderSwagger(getSpecUrl(selectedVersion), "");
  }

  function handleVersionChange(version: string) {
    setSelectedVersion(version);
    renderSwagger(getSpecUrl(version), baseUrl);
  }

  return (
    <div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: "0.5rem",
          padding: "0.75rem 1rem",
          marginBottom: "0",
          background: "var(--ifm-color-emphasis-100)",
          borderRadius: "var(--ifm-border-radius)",
          flexWrap: "wrap",
        }}
      >
        <label style={{ fontWeight: 600, whiteSpace: "nowrap" }}>
          B4 Instance:
        </label>
        <input
          type="text"
          value={inputValue}
          onChange={(e) => setInputValue(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleConnect()}
          placeholder="http://192.168.1.1:7000"
          style={{
            flex: 1,
            minWidth: "200px",
            padding: "0.4rem 0.6rem",
            border: "1px solid var(--ifm-color-emphasis-300)",
            borderRadius: "var(--ifm-border-radius)",
            fontSize: "0.9rem",
            fontFamily: "monospace",
          }}
        />
        <button
          onClick={handleConnect}
          className="button button--primary button--sm"
        >
          {baseUrl ? "Reconnect" : "Connect"}
        </button>
        {baseUrl && (
          <button
            onClick={handleDisconnect}
            className="button button--outline button--secondary button--sm"
          >
            Disconnect
          </button>
        )}
        <span
          style={{
            fontSize: "0.8rem",
            color: baseUrl
              ? "var(--ifm-color-success)"
              : "var(--ifm-color-emphasis-600)",
          }}
        >
          {baseUrl
            ? `Connected to ${baseUrl}`
            : "Read-only mode (no API calls)"}
        </span>

        {versions.length > 0 && (
          <>
            <div
              style={{
                width: "1px",
                height: "1.5rem",
                background: "var(--ifm-color-emphasis-300)",
                margin: "0 0.25rem",
              }}
            />
            <label style={{ fontWeight: 600, whiteSpace: "nowrap" }}>
              API Version:
            </label>
            <select
              value={selectedVersion}
              onChange={(e) => handleVersionChange(e.target.value)}
              style={{
                padding: "0.4rem 0.6rem",
                border: "1px solid var(--ifm-color-emphasis-300)",
                borderRadius: "var(--ifm-border-radius)",
                fontSize: "0.9rem",
              }}
            >
              <option value="latest">latest</option>
              {versions.map((v: string) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
            </select>
          </>
        )}
      </div>
      <iframe
        ref={iframeRef}
        style={{
          width: "100%",
          height: "calc(100vh - 120px)",
          border: "none",
        }}
        title="Swagger UI"
      />
    </div>
  );
}

export default function SwaggerPage() {
  return (
    <Layout title="API Reference" description="B4 REST API documentation">
      <main style={{ padding: "1rem" }}>
        <BrowserOnly>{() => <SwaggerUILoader />}</BrowserOnly>
      </main>
    </Layout>
  );
}
