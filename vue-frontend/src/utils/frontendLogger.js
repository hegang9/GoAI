const DEBUG_FLAG = "gopherai:frontend-debug";
const DEBUG_PREFIX = "[GopherAI-FE]";

// 前端临时诊断日志：开发环境默认开启，生产环境需 localStorage 手动开启，避免噪声进入正式用户控制台。
function isDebugEnabled() {
  return (
    process.env.NODE_ENV !== "production" ||
    localStorage.getItem(DEBUG_FLAG) === "1"
  );
}

function buildPayload(scope, payload) {
  return {
    scope,
    at: new Date().toISOString(),
    ...payload,
  };
}

export function logFrontendEvent(scope, payload = {}) {
  if (!isDebugEnabled()) return;
  console.info(DEBUG_PREFIX, buildPayload(scope, payload));
}

export function logFrontendError(scope, error, payload = {}) {
  if (!isDebugEnabled()) return;
  console.error(
    DEBUG_PREFIX,
    buildPayload(scope, {
      ...payload,
      errorMessage: error?.message || String(error),
      errorStack: error?.stack || "",
    }),
  );
}

export {DEBUG_FLAG, DEBUG_PREFIX};
