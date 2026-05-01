export function withBasePath(value: string) {
  return `${getAppBasePath()}${normalizeAppPath(value)}`;
}

export function getLoginRoute() {
  return withBasePath("/login/");
}

export function getProtectedAppRoute() {
  return withBasePath("/app/");
}

function getAppBasePath() {
  if (typeof document === "undefined") {
    return "";
  }

  const declaredBasePath = document
    .querySelector('meta[name="app-base-path"]')
    ?.getAttribute("content")
    ?.trim();

  if (!declaredBasePath || declaredBasePath === "__APP_BASE_PATH__" || declaredBasePath === "/") {
    return "";
  }

  return `/${declaredBasePath.replace(/^\/+|\/+$/g, "")}`;
}

function normalizeAppPath(value: string) {
  if (!value) {
    return "/";
  }

  return value.startsWith("/") ? value : `/${value}`;
}
