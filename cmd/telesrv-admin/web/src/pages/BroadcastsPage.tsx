import { ChevronLeft, ChevronRight, Megaphone, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { Alert, Badge, EmptyRow, Metric, PageFrame, QueryPanel } from "../components/ui";
import { useI18n } from "../i18n";
import { formatDate } from "../lib/format";
import type { BroadcastListResponse, BroadcastRow } from "../types";
import { CreateBroadcastModal } from "./CreateBroadcastModal";

function status(row: BroadcastRow): "enumerating" | "delivering" | "completed" {
  if (!row.EnumerationDone) return "enumerating";
  if (BigInt(row.SentCount) + BigInt(row.FailedCount) < BigInt(row.MaterializedCount)) return "delivering";
  return "completed";
}

export function BroadcastsPage() {
  const { t } = useI18n();
  const [data, setData] = useState<BroadcastListResponse | null>(null);
  const [cursor, setCursor] = useState("0");
  const [cursorHistory, setCursorHistory] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [createOpen, setCreateOpen] = useState(false);

  async function loadPage(target: string, resetHistory = false) {
    setBusy(true);
    setError("");
    const params = new URLSearchParams({ limit: "50" });
    if (target !== "0") params.set("before_id", target);
    try {
      const result = await api.broadcasts(params);
      setData(result);
      setCursor(target);
      if (resetHistory) setCursorHistory([]);
      return true;
    } catch (err) {
      setError(errorMessage(err));
      return false;
    } finally {
      setBusy(false);
    }
  }

  async function nextPage() {
    if (!data?.has_more) return;
    if (await loadPage(data.next_before_id)) setCursorHistory((history) => [...history, cursor]);
  }

  async function previousPage() {
    const target = cursorHistory[cursorHistory.length - 1];
    if (target === undefined) return;
    if (await loadPage(target)) setCursorHistory((history) => history.slice(0, -1));
  }

  useEffect(() => { void loadPage("0", true); }, []);

  const rows = data?.rows ?? [];
  const active = rows.filter((row) => status(row) !== "completed").length;
  const failed = rows.reduce((sum, row) => sum + Number(row.FailedCount), 0);

  return (
    <PageFrame
      title={t("broadcast.pageTitle")}
      eyebrow={t("broadcast.pageHint")}
      actions={<div className="toolbar">
        <button className="btn warn" type="button" onClick={() => setCreateOpen(true)}><Megaphone size={15} /> {t("broadcast.create")}</button>
        <button className="btn" type="button" onClick={() => loadPage(cursor)} disabled={busy}><RefreshCw size={15} /> {t("common.refresh")}</button>
      </div>}
    >
      {error && <Alert>{error}</Alert>}
      {createOpen && <CreateBroadcastModal onClose={() => setCreateOpen(false)} onCreated={() => loadPage("0", true)} />}
      <div className="metric-row">
        <Metric label={t("broadcast.currentPage")} value={String(rows.length)} />
        <Metric label={t("broadcast.active")} value={String(active)} />
        <Metric label={t("broadcast.failed")} value={String(failed)} tone={failed > 0 ? "danger" : "neutral"} />
      </div>
      <QueryPanel>
        <div className="toolbar">
          {cursorHistory.length > 0 && <button className="btn icon-text" type="button" onClick={previousPage} disabled={busy}><ChevronLeft size={15} /> {t("messages.previousPage")}</button>}
          {data?.has_more && <button className="btn icon-text" type="button" onClick={nextPage} disabled={busy}><ChevronRight size={15} /> {t("messages.nextPage")}</button>}
        </div>
      </QueryPanel>
      <div className="table-wrap">
        <table className="data-table">
          <thead><tr>
            <th>{t("broadcast.id")}</th><th>{t("broadcast.message")}</th><th>{t("broadcast.target")}</th>
            <th>{t("broadcast.progress")}</th><th>{t("broadcast.status")}</th><th>{t("broadcast.createdBy")}</th><th>{t("account.createdAt")}</th>
          </tr></thead>
          <tbody>
            {rows.map((row) => {
              const state = status(row);
              return <tr key={row.ID}>
                <td className="mono">{row.ID}</td>
                <td title={row.Message}>{row.Message.length > 100 ? `${row.Message.slice(0, 100)}…` : row.Message}</td>
                <td><Badge>{t(row.TargetMode === "all" ? "broadcast.targetAll" : "broadcast.targetSelected")}</Badge></td>
                <td>{row.SentCount} / {row.TargetCount}{BigInt(row.FailedCount) > 0n ? ` · ${t("broadcast.failed")} ${row.FailedCount}` : ""}</td>
                <td><Badge tone={state === "completed" ? "good" : state === "delivering" ? "warn" : "neutral"}>{t(`broadcast.${state}`)}</Badge></td>
                <td>{row.CreatedBy || "-"}</td><td>{formatDate(row.CreatedAt)}</td>
              </tr>;
            })}
            {rows.length === 0 && <EmptyRow colSpan={7} />}
          </tbody>
        </table>
      </div>
    </PageFrame>
  );
}
