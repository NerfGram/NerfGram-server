import { Plus, X } from "lucide-react";
import { useState } from "react";
import { createPortal } from "react-dom";
import { ActionButton } from "../components/ActionButton";
import { useI18n } from "../i18n";
import { toInt } from "../lib/format";

export function CreateBotModal({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const { t } = useI18n();
  const [ownerID, setOwnerID] = useState("");
  const [botName, setBotName] = useState("");
  const [botUsername, setBotUsername] = useState("");

  const username = botUsername.trim().replace(/^@/, "");
  const valid = toInt(ownerID) > 0 && botName.trim().length > 0 && /^[A-Za-z0-9_]{2,29}bot$/i.test(username);

  return createPortal(
    <div className="modal-backdrop" role="presentation">
      <section className="modal command-modal" role="dialog" aria-modal="true" aria-label={t("bots.createTitle")}>
        <div className="modal-head">
          <div>
            <div className="eyebrow">{t("layout.bots")}</div>
            <h2>{t("bots.createTitle")}</h2>
            <p className="bot-create-note">{t("bots.createHint")}</p>
          </div>
          <button className="icon-btn" type="button" onClick={onClose} aria-label={t("common.close")}><X size={15} /></button>
        </div>
        <div className="command-body">
          <div className="bot-create-fields">
            <label className="duration-field">
              <span>{t("bots.ownerUserID")}</span>
              <input value={ownerID} onChange={(event) => setOwnerID(event.target.value)} type="number" min="1" placeholder="123456789" />
            </label>
            <label className="duration-field">
              <span>{t("bots.name")}</span>
              <input value={botName} onChange={(event) => setBotName(event.target.value)} placeholder={t("bots.namePlaceholder")} maxLength={64} />
            </label>
            <label className="duration-field">
              <span>{t("bots.username")}</span>
              <input value={botUsername} onChange={(event) => setBotUsername(event.target.value)} placeholder="my_service_bot" />
            </label>
          </div>
          <p className="bot-create-note">{t("bots.usernameHint")}</p>
          <p className="bot-create-note">{t("bots.tokenResultHint")}</p>
        </div>
        <div className="modal-actions">
          <button className="btn" type="button" onClick={onClose}>{t("common.close")}</button>
          <ActionButton
            label={t("bots.create")}
            icon={<Plus size={15} />}
            tone="neutral"
            disabled={!valid}
            path="/api/actions/create-bot"
            secretField="token"
            payload={() => ({ owner_user_id: toInt(ownerID), name: botName.trim(), username })}
            onDone={onCreated}
          />
        </div>
      </section>
    </div>,
    document.body
  );
}
