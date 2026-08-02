export type PlanAction = {
  op?: string;
  backend?: string;
  kind?: string;
  name?: string;
  namespace?: string;
  cluster_context?: string;
  replicas?: number;
  revision?: number;
  diff?: string;
};

export type PlanResultDoc = {
  apiVersion?: string;
  kind?: string;
  schemaVersion?: string;
  prompt?: string;
  cluster_context?: string;
  plan?: {
    intent?: string;
    summary?: string;
    requiresApproval?: boolean;
    namespace?: string;
    context?: string;
    actions?: PlanAction[];
  };
  risk?: {
    level?: string;
    denied?: boolean;
    message?: string;
  };
  applied?: boolean;
};

export function parsePlanResult(raw: string): PlanResultDoc | null {
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!data || typeof data !== "object" || Array.isArray(data)) {
    return null;
  }
  const o = data as Record<string, unknown>;
  // Accept PlanResult or CI wrappers that nest it.
  if (o.kind === "PlanResult" || o.plan != null || o.risk != null) {
    return o as PlanResultDoc;
  }
  return null;
}

export function isMutatingPlan(doc: PlanResultDoc): boolean {
  if (doc.plan?.requiresApproval) return true;
  const actions = doc.plan?.actions ?? [];
  return actions.some((a) => {
    const op = (a.op ?? "").toLowerCase();
    return op !== "" && op !== "get" && op !== "list" && op !== "explain";
  });
}
