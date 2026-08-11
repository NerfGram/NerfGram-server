import { Check, Copy, X } from "lucide-react";
import { useState } from "react";
import { createPortal } from "react-dom";
import { api, errorMessage } from "../api";
import { useI18n } from "../i18n";
import { Alert } from "./ui";

export function CopyBotTokenModal({ botID, onClose }: { botID: number; onClose: () => void }) {
  const { t } = useI18n();
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);

  async function copyToken() {
    if (!reason.trim()) {
      setError(t("action.reasonRequired"));
      return;
    }
    setBusy(true);
    setError("");
    setCopied(false);
    try {
      const result = await api.action("/api/actions/export-bot-token", { reason: reason.trim(), confirm: true, bot_user_id: botID });
      const token = result.details?.token;
      if (result.error || typeof token !== "string" || !token) {
        setError(result.error || t("bots.noTokenReturned"));
        return;
      }
      await navigator.clipboard.writeText(token);
      setCopied(true);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return createPortal(
    <div className="modal-backdrop" role="presentation">
      <section className="modal command-modal" role="dialog" aria-modal="true" aria-label={t("bots.copyToken")}>
        <div className="modal-head">
          <div><div className="eyebrow">{t("common.bot")}</div><h2>{t("bots.copyToken")}</h2></div>
          <button className="icon-btn" type="button" onClick={onClose} disabled={busy} aria-label={t("common.close")}><X size={15} /></button>
        </div>
        <div className="command-body">
          <p>{t("bots.copyTokenHint")}</p>
          <label className="form-field"><span>{t("action.reason")}</span><textarea value={reason} onChange={(event) => setReason(event.target.value)} rows={3} /></label>
          {error && <Alert>{error}</Alert>}
          {copied && <div className="secret-reveal"><div className="secret-reveal-label"><Check size={14} /> {t("bots.tokenCopied")}</div></div>}
        </div>
        <div className="modal-actions">
          <button className="btn" type="button" onClick={onClose} disabled={busy}>{t("common.close")}</button>
          <button className="btn primary icon-text" type="button" onClick={() => void copyToken()} disabled={busy}><Copy size={15} />{copied ? t("bots.copyAgain") : t("bots.copyToken")}</button>
        </div>
      </section>
    </div>,
    document.body
  );
}
