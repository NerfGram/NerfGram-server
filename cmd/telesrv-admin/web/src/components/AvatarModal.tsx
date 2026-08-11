import { ImagePlus, Loader2, Upload, X } from "lucide-react";
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { api, errorMessage } from "../api";
import { useI18n } from "../i18n";
import { Alert } from "./ui";

export function AvatarModal({ kind, id, onClose, onDone }: {
  kind: "user" | "channel";
  id: number;
  onClose: () => void;
  onDone: () => void;
}) {
  const { t } = useI18n();
  const [file, setFile] = useState<File | null>(null);
  const [previewURL, setPreviewURL] = useState("");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!file) {
      setPreviewURL("");
      return;
    }
    const url = URL.createObjectURL(file);
    setPreviewURL(url);
    return () => URL.revokeObjectURL(url);
  }, [file]);

  async function submit() {
    if (!file || !reason.trim()) {
      setError(!file ? t("avatar.chooseFirst") : t("action.reasonRequired"));
      return;
    }
    setBusy(true);
    setError("");
    try {
      const form = new FormData();
      const idField = kind === "channel" ? "channel_id" : "user_id";
      form.set("metadata", JSON.stringify({ command_id: "", reason: reason.trim(), confirm: true, [idField]: id }));
      form.set("file", file, file.name);
      const result = kind === "channel" ? await api.setChannelAvatar(form) : await api.setAccountAvatar(form);
      if (result.error) {
        setError(result.error);
        return;
      }
      onDone();
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return createPortal(
    <div className="modal-backdrop" role="presentation">
      <section className="modal command-modal" role="dialog" aria-modal="true" aria-label={t("avatar.change")}>
        <div className="modal-head">
          <div><div className="eyebrow">{kind === "channel" ? t("common.channel") : t("common.account")}</div><h2>{t("avatar.change")}</h2></div>
          <button className="icon-btn" type="button" onClick={onClose} disabled={busy} aria-label={t("common.close")}><X size={15} /></button>
        </div>
        <div className="command-body">
          <label className={`gift-file-picker ${file ? "has-file" : ""}`}>
            <input type="file" accept="image/png,image/jpeg,image/webp" onChange={(event) => setFile(event.target.files?.[0] ?? null)} />
            {previewURL ? <img className="gift-file-icon" src={previewURL} alt="" style={{ objectFit: "cover" }} /> : <ImagePlus size={22} />}
            <span className="gift-file-copy"><span className="gift-field-label">{t("avatar.new")}</span><strong>{file ? file.name : t("avatar.chooseImage")}</strong></span>
            <span className="gift-file-action">{file ? t("common.changeFile") : t("common.chooseFile")}</span>
          </label>
          <label className="gift-reason-field"><span>{t("action.reason")}</span><input value={reason} placeholder={t("avatar.reasonPlaceholder")} onChange={(event) => setReason(event.target.value)} /></label>
          {error && <Alert>{error}</Alert>}
        </div>
        <div className="modal-actions">
          <button className="btn" type="button" onClick={onClose} disabled={busy}>{t("common.close")}</button>
          <button className="btn primary icon-text" type="button" onClick={() => void submit()} disabled={busy}>
            {busy ? <Loader2 className="spin" size={15} /> : <Upload size={15} />} {t("avatar.upload")}
          </button>
        </div>
      </section>
    </div>,
    document.body
  );
}
