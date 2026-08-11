import { Megaphone, X } from "lucide-react";
import { useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { ActionButton } from "../components/ActionButton";
import { useI18n } from "../i18n";

function parseUserIDs(raw: string): number[] | null {
  const parts = raw.split(/[\s,]+/).filter(Boolean);
  if (parts.length === 0 || parts.length > 200) return null;
  const ids = parts.map((part) => Number(part));
  if (ids.some((id) => !Number.isSafeInteger(id) || id <= 0)) return null;
  if (new Set(ids).size !== ids.length) return null;
  return ids;
}

export function CreateBroadcastModal({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const { t } = useI18n();
  const [message, setMessage] = useState("");
  const [targetMode, setTargetMode] = useState<"all" | "selected">("all");
  const [rawUserIDs, setRawUserIDs] = useState("");
  const selectedUserIDs = useMemo(() => parseUserIDs(rawUserIDs), [rawUserIDs]);
  const messageBytes = new TextEncoder().encode(message.trim()).length;
  const valid = message.trim().length > 0 && messageBytes <= 4096 && (targetMode === "all" || selectedUserIDs !== null);

  return createPortal(
    <div className="modal-backdrop" role="presentation">
      <section className="modal command-modal" role="dialog" aria-modal="true" aria-label={t("broadcast.createTitle")}>
        <div className="modal-head">
          <div>
            <div className="eyebrow">{t("layout.broadcasts")}</div>
            <h2>{t("broadcast.createTitle")}</h2>
            <p className="bot-create-note">{t("broadcast.createHint")}</p>
          </div>
          <button className="icon-btn" type="button" onClick={onClose} aria-label={t("common.close")}><X size={15} /></button>
        </div>
        <div className="command-body">
          <label className="form-field">
            <span>{t("broadcast.message")}</span>
            <textarea rows={7} maxLength={4096} value={message} onChange={(event) => setMessage(event.target.value)} placeholder={t("broadcast.messagePlaceholder")} />
            <small className="bot-create-note">{t("broadcast.messageBytes", { count: messageBytes })}</small>
          </label>
          <label className="form-field">
            <span>{t("broadcast.target")}</span>
            <select value={targetMode} onChange={(event) => setTargetMode(event.target.value as "all" | "selected")}>
              <option value="all">{t("broadcast.targetAll")}</option>
              <option value="selected">{t("broadcast.targetSelected")}</option>
            </select>
          </label>
          {targetMode === "selected" && (
            <label className="form-field">
              <span>{t("broadcast.userIDs")}</span>
              <textarea rows={4} value={rawUserIDs} onChange={(event) => setRawUserIDs(event.target.value)} placeholder="123456789, 123456790" />
              <small className="bot-create-note">{t("broadcast.userIDsHint")}</small>
            </label>
          )}
        </div>
        <div className="modal-actions">
          <button className="btn" type="button" onClick={onClose}>{t("common.close")}</button>
          <ActionButton
            label={t("broadcast.create")}
            icon={<Megaphone size={15} />}
            tone="warn"
            disabled={!valid}
            path="/api/actions/create-broadcast"
            payload={() => ({
              message: message.trim(),
              target_mode: targetMode,
              user_ids: targetMode === "selected" ? selectedUserIDs ?? [] : []
            })}
            onDone={onCreated}
          />
        </div>
      </section>
    </div>,
    document.body
  );
}
