import { KeyRound, Loader2, RefreshCw, Search } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { api, errorMessage } from "../api";
import { Alert, Badge, EmptyRow, Metric, PageFrame, QueryPanel } from "../components/ui";
import { useI18n } from "../i18n";
import type { ActivePhoneCode } from "../types";

function formatTTL(seconds: number): string {
  if (seconds <= 0) return "—";
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  if (m < 60) return s ? `${m}m ${s}s` : `${m}m`;
  const h = Math.floor(m / 60);
  const rm = m % 60;
  return rm ? `${h}h ${rm}m` : `${h}h`;
}

function recipient(row: ActivePhoneCode): string {
  return row.email || row.pending_email || row.phone || "—";
}

function purposeLabel(row: ActivePhoneCode, loginLabel: string): string {
  return row.purpose || loginLabel;
}

export function CodesPage() {
  const { t } = useI18n();
  const [rows, setRows] = useState<ActivePhoneCode[]>([]);
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function load() {
    setBusy(true);
    setError("");
    try {
      setRows((await api.codes()).rows ?? []);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const visible = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) return rows;
    return rows.filter((row) => {
      const haystack = [
        row.hash,
        row.code,
        row.phone,
        row.email,
        row.pending_email,
        row.channel,
        row.purpose,
        row.user_id,
        row.issued_user_id
      ].join(" ").toLowerCase();
      return haystack.includes(normalized);
    });
  }, [rows, query]);

  const metrics = useMemo(() => {
    const login = rows.filter((row) => !row.purpose).length;
    const scoped = rows.length - login;
    const soon = rows.filter((row) => row.ttl_seconds > 0 && row.ttl_seconds <= 60).length;
    return { login, scoped, soon };
  }, [rows]);

  return (
    <PageFrame
      title={t("codes.pageTitle")}
      eyebrow={t("codes.eyebrow")}
      actions={
        <button className="btn icon-text" type="button" onClick={() => void load()} disabled={busy}>
          {busy ? <Loader2 size={15} className="spin" /> : <RefreshCw size={15} />} {t("common.refresh")}
        </button>
      }
    >
      {error && <Alert>{error}</Alert>}
      <div className="metric-row">
        <Metric label={t("codes.active")} value={String(rows.length)} />
        <Metric label={t("codes.login")} value={String(metrics.login)} />
        <Metric label={t("codes.scoped")} value={String(metrics.scoped)} />
        <Metric label={t("codes.expiringSoon")} value={String(metrics.soon)} tone={metrics.soon > 0 ? "warn" : "neutral"} />
      </div>
      <QueryPanel>
        <form className="toolbar" onSubmit={(event) => event.preventDefault()}>
          <label className="searchbox">
            <Search size={15} />
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("codes.searchPlaceholder")} />
          </label>
        </form>
      </QueryPanel>
      <div className="table-wrap">
        <table className="data-table">
          <thead>
            <tr>
              <th>{t("codes.code")}</th>
              <th>{t("codes.recipient")}</th>
              <th>{t("codes.channel")}</th>
              <th>{t("codes.purpose")}</th>
              <th>{t("codes.user")}</th>
              <th>{t("codes.attempts")}</th>
              <th>{t("codes.ttl")}</th>
              <th>{t("codes.flags")}</th>
              <th>{t("codes.hash")}</th>
            </tr>
          </thead>
          <tbody>
            {visible.map((row) => (
              <tr key={row.hash}>
                <td className="mono">
                  <span className="icon-text"><KeyRound size={14} /> {row.code || "—"}</span>
                </td>
                <td>{recipient(row)}</td>
                <td><Badge>{row.channel || "—"}</Badge></td>
                <td>{purposeLabel(row, t("codes.loginPurpose"))}</td>
                <td className="mono">
                  {row.user_id !== "0" && row.user_id ? row.user_id : row.issued_user_id !== "0" && row.issued_user_id ? row.issued_user_id : "—"}
                </td>
                <td className="mono">{row.attempts}/{row.max_attempts || "—"}</td>
                <td className="mono">{formatTTL(row.ttl_seconds)}</td>
                <td>
                  {row.sign_up_verified && <Badge tone="good">{t("codes.signUpVerified")}</Badge>}
                  {row.require_sign_up && <Badge tone="warn">{t("codes.requireSignUp")}</Badge>}
                  {row.verified_email && <Badge>{t("common.verified")}</Badge>}
                  {!row.sign_up_verified && !row.require_sign_up && !row.verified_email ? <Badge>{t("common.none")}</Badge> : null}
                </td>
                <td className="mono" title={row.hash}>{row.hash.length > 16 ? `${row.hash.slice(0, 8)}…${row.hash.slice(-6)}` : row.hash}</td>
              </tr>
            ))}
            {visible.length === 0 && <EmptyRow colSpan={9} />}
          </tbody>
        </table>
      </div>
    </PageFrame>
  );
}
