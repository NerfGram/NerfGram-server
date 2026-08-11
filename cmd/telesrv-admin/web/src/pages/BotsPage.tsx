import { BadgeCheck, Bot, ChevronLeft, ChevronRight, Loader2, Plus, RefreshCw, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { Alert, Badge, EmptyRow, Metric, PageFrame, QueryPanel } from "../components/ui";
import { ScamFakeBadges } from "../components/flags";
import { Avatar } from "../components/Avatar";
import { useI18n } from "../i18n";
import { displayUsername, formatDate } from "../lib/format";
import type { Navigate } from "../routing";
import type { BotListResponse } from "../types";
import { CreateBotModal } from "./CreateBotModal";

export function BotsPage({ navigate }: { navigate: Navigate }) {
  const { t } = useI18n();
  const [q, setQ] = useState("");
  const [limit, setLimit] = useState("50");
  const [data, setData] = useState<BotListResponse | null>(null);
  const [filters, setFilters] = useState({ q: "", limit: "50" });
  const [cursor, setCursor] = useState(0);
  const [cursorHistory, setCursorHistory] = useState<number[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const [createOpen, setCreateOpen] = useState(false);

  async function loadPage(target: number, requestedFilters = filters, resetHistory = false) {
    setBusy(true);
    setError("");
    const params = new URLSearchParams({ limit: requestedFilters.limit });
    if (requestedFilters.q) {
      params.set("q", requestedFilters.q);
    } else if (target > 0) {
      params.set("before_id", String(target));
    }
    try {
      const result = await api.bots(params);
      setData(result);
      setFilters(requestedFilters);
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

  async function search() {
    await loadPage(0, { q: q.trim(), limit }, true);
  }

  async function nextPage() {
    if (!data?.listing || !data.has_more) return;
    if (await loadPage(data.next_before_id)) setCursorHistory((history) => [...history, cursor]);
  }

  async function previousPage() {
    const target = cursorHistory[cursorHistory.length - 1];
    if (target === undefined) return;
    if (await loadPage(target)) setCursorHistory((history) => history.slice(0, -1));
  }

  useEffect(() => {
    void loadPage(0, { q: "", limit: "50" }, true);
  }, []);

  const rows = data?.rows ?? [];
  const verified = rows.filter((row) => row.Verified).length;
  const systemCount = rows.filter((row) => row.System).length;

  return (
    <PageFrame
      title={t("bots.pageTitle")}
      eyebrow={data?.listing === false ? t("bots.queryResults") : t("bots.recent")}
      actions={
        <div className="toolbar">
          <button className="btn" type="button" onClick={() => setCreateOpen(true)}><Plus size={15} /> {t("bots.create")}</button>
          <button className="btn" type="button" onClick={() => loadPage(cursor)} disabled={busy}>
            <RefreshCw size={15} /> {t("common.refresh")}
          </button>
        </div>
      }
    >
      {error && <Alert>{error}</Alert>}
      <div className="metric-row">
        <Metric label={t("bots.currentPage")} value={String(rows.length)} />
        <Metric label={t("common.verified")} value={String(verified)} tone="good" />
        <Metric label={t("bots.system")} value={String(systemCount)} />
      </div>

      {createOpen && <CreateBotModal onClose={() => setCreateOpen(false)} onCreated={() => loadPage(0, { q: "", limit }, true)} />}

      <QueryPanel>
        <form className="toolbar" onSubmit={(event) => { event.preventDefault(); void search(); }}>
          <label className="searchbox">
            <Search size={15} />
            <input value={q} onChange={(event) => setQ(event.target.value)} placeholder={t("bots.searchPlaceholder")} />
          </label>
          <label className="field-inline">
            <span>{t("common.limit")}</span>
            <input className="small-input" value={limit} onChange={(event) => setLimit(event.target.value)} type="number" min="1" max="100" />
          </label>
          <button className="btn primary icon-text" type="submit" disabled={busy}>
            {busy ? <Loader2 size={15} className="spin" /> : <Search size={15} />} {t("common.search")}
          </button>
          {data?.listing && cursorHistory.length > 0 && (
            <button className="btn icon-text" type="button" onClick={() => previousPage()} disabled={busy}>
              <ChevronLeft size={15} /> {t("messages.previousPage")}
            </button>
          )}
          {data?.listing && data.has_more && (
            <button className="btn icon-text" type="button" onClick={() => nextPage()} disabled={busy}>
              <ChevronRight size={15} /> {t("messages.nextPage")}
            </button>
          )}
        </form>
      </QueryPanel>

      <div className="table-wrap">
        <table className="data-table">
          <thead>
            <tr>
              <th>{t("bots.botID")}</th>
              <th>{t("common.username")}</th>
              <th>{t("common.name")}</th>
              <th>{t("bots.owner")}</th>
              <th>{t("common.verified")}</th>
              <th>{t("bots.type")}</th>
              <th>{t("account.createdAt")}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.ID}>
                <td className="mono">{row.ID}</td>
                <td>{displayUsername(row.Username) || "-"}</td>
                <td><span className="table-identity"><Avatar id={row.ID} label={`${row.FirstName} ${row.LastName}`.trim() || row.Username} />{`${row.FirstName} ${row.LastName}`.trim() || "-"}</span></td>
                <td className="mono">{row.OwnerUserID > 0 ? row.OwnerUserID : "-"}</td>
                <td>{row.Verified ? <Badge tone="good"><BadgeCheck size={12} /> {t("common.verified")}</Badge> : <Badge>{t("account.notVerified")}</Badge>} <ScamFakeBadges scam={row.Scam} fake={row.Fake} /></td>
                <td>{row.System ? <Badge tone="warn">{t("bots.system")}</Badge> : <Badge>{t("bots.user")}</Badge>}</td>
                <td>{formatDate(row.CreatedAt)}</td>
                <td><button className="row-link" onClick={() => navigate(`/bots/${row.ID}`)}><Bot size={14} /> {t("common.detail")} <ChevronRight size={14} /></button></td>
              </tr>
            ))}
            {rows.length === 0 && <EmptyRow colSpan={8} />}
          </tbody>
        </table>
      </div>
    </PageFrame>
  );
}
