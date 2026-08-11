import { ChevronLeft, ChevronRight, Loader2, RefreshCw, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { Alert, Badge, EmptyRow, Metric, PageFrame, QueryPanel } from "../components/ui";
import { ScamFakeBadges } from "../components/flags";
import { Avatar } from "../components/Avatar";
import { useI18n } from "../i18n";
import { channelKind, displayUsername, formatDate } from "../lib/format";
import { channelMetrics } from "../lib/metrics";
import type { Navigate } from "../routing";
import type { ChannelListResponse } from "../types";

type ChannelCursor = { beforeID: number; beforeUpdatedUS: number };

const firstChannelPage: ChannelCursor = { beforeID: 0, beforeUpdatedUS: 0 };

export function ChannelsPage({ navigate }: { navigate: Navigate }) {
  const { t } = useI18n();
  const [q, setQ] = useState("");
  const [limit, setLimit] = useState("50");
  const [data, setData] = useState<ChannelListResponse | null>(null);
  const [filters, setFilters] = useState({ q: "", limit: "50" });
  const [cursor, setCursor] = useState<ChannelCursor>(firstChannelPage);
  const [cursorHistory, setCursorHistory] = useState<ChannelCursor[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function loadPage(target: ChannelCursor, requestedFilters = filters, resetHistory = false) {
    setBusy(true);
    setError("");
    const params = new URLSearchParams({ limit: requestedFilters.limit });
    if (requestedFilters.q) {
      params.set("q", requestedFilters.q);
    } else if (target.beforeID > 0) {
      params.set("before_id", String(target.beforeID));
      params.set("before_updated_us", String(target.beforeUpdatedUS));
    }
    try {
      const result = await api.channels(params);
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
    const requestedFilters = { q: q.trim(), limit };
    await loadPage(firstChannelPage, requestedFilters, true);
  }

  async function nextPage() {
    if (!data?.listing || !data.has_more) return;
    const target = { beforeID: data.next_before_id, beforeUpdatedUS: data.next_before_updated_us };
    if (await loadPage(target)) setCursorHistory((history) => [...history, cursor]);
  }

  async function previousPage() {
    const target = cursorHistory[cursorHistory.length - 1];
    if (!target) return;
    if (await loadPage(target)) setCursorHistory((history) => history.slice(0, -1));
  }

  useEffect(() => {
    void loadPage(firstChannelPage, { q: "", limit: "50" }, true);
  }, []);

  const metrics = channelMetrics(data?.rows ?? []);

  return (
    <PageFrame
      title={t("channel.pageTitle")}
      eyebrow={data?.listing === false ? t("account.queryResults") : t("channel.recentUpdated")}
      actions={
        <button className="btn" type="button" onClick={() => loadPage(cursor)} disabled={busy}>
          <RefreshCw size={15} /> {t("common.refresh")}
        </button>
      }
    >
      {error && <Alert>{error}</Alert>}
      <div className="metric-row">
        <Metric label={t("channel.currentPage")} value={String(data?.rows.length ?? 0)} />
        <Metric label={t("channel.megagroups")} value={String(metrics.megagroups)} />
        <Metric label={t("channel.broadcasts")} value={String(metrics.broadcasts)} />
        <Metric label={t("channel.verifiedCount")} value={String(metrics.verified)} tone="good" />
      </div>
      <QueryPanel>
        <form className="toolbar" onSubmit={(event) => { event.preventDefault(); void search(); }}>
          <label className="searchbox">
            <Search size={15} />
            <input value={q} onChange={(event) => setQ(event.target.value)} placeholder={t("channel.searchPlaceholder")} />
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
              <th>{t("channel.channelID")}</th>
              <th>{t("channel.kind")}</th>
              <th>{t("common.username")}</th>
              <th>{t("channel.title")}</th>
              <th>{t("common.members")}</th>
              <th>{t("common.admins")}</th>
              <th>PTS</th>
              <th>{t("common.verified")}</th>
              <th>{t("common.updatedAt")}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {data?.rows.map((row) => (
              <tr key={row.ID}>
                <td className="mono">{row.ID}</td>
                <td>{channelKind(row, t)}</td>
                <td>{displayUsername(row.Username)}</td>
                <td><span className="table-identity"><Avatar id={row.ID} kind="channel" label={row.Title || row.Username} />{row.Title}</span></td>
                <td>{row.ParticipantsCount}</td>
                <td>{row.AdminsCount}</td>
                <td>{row.PTS}</td>
                <td>{row.Verified ? <Badge tone="good">{t("common.verified")}</Badge> : <Badge>{t("account.notVerified")}</Badge>} <ScamFakeBadges scam={row.Scam} fake={row.Fake} /></td>
                <td>{formatDate(row.UpdatedAt)}</td>
                <td><button className="row-link" onClick={() => navigate(`/channels/${row.ID}`)}>{t("common.detail")} <ChevronRight size={14} /></button></td>
              </tr>
            ))}
            {(!data || data.rows.length === 0) && <EmptyRow colSpan={10} />}
          </tbody>
        </table>
      </div>
    </PageFrame>
  );
}
