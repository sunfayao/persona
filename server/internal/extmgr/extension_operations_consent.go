package extmgr

import (
	"time"

	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/i18n"
)

// opPreviewed records the preview while the operation waits for consent.
func (h *Manager) opPreviewed(op *extOp, pv *extension.Preview) {
	h.update(op, func(v *ExtensionOperation) {
		m := pv.Manifest
		v.Extension, v.Name, v.Version = m.ID, m.Name, m.Version
		if pv.Upgrade {
			v.Kind, v.From = opUpgrade, pv.From
		}
		if pv.Market != "" {
			v.Market, v.Source = pv.Market, extension.MarketOrigin(pv.Market, m.ID)
		}
		setStep(v, extension.StepCheck, extension.StepDone)
		v.PreviewID = pv.ID
		setStep(v, extension.StepConsent, extension.StepRunning)
		v.State = opNeedsConsent
	})
}

// claimOpOfPreview takes the operation waiting for consent with preview previewID for one install, nil when none
// is. It is marked running at once, under the lock, so a second install of the same preview (a double click, the
// web and the CLI together) cannot go on with it: that one is an installing_already error while the claimed one runs, and
// never reaches the registry, so it can neither end the install nor take the preview from it.
func (h *Manager) claimOpOfPreview(previewID string) (*extOp, error) {
	if previewID == "" {
		return nil, nil
	}
	ps := h.extensions
	ps.ops.mu.Lock()
	defer ps.ops.mu.Unlock()
	for _, op := range ps.ops.byID {
		switch {
		case op.v.PreviewID == previewID && op.v.State == opNeedsConsent:
			op.v.State, op.v.PreviewID, op.v.UpdatedAt = opRunning, "", time.Now()
			op.claimed = previewID
			return op, nil
		case op.claimed == previewID && op.v.State == opRunning:
			return nil, i18n.Err("hub-extension_operations.installing_already")
		}
	}
	return nil, nil
}

// claimedByMarket says whether an install of a market's extension took over the operation that was waiting for consent
// with preview previewID (claimOpOfPreview): the preview was that market's, also once the install has used it up. It
// is known only while that operation is kept.
func (h *Manager) claimedByMarket(previewID string) bool {
	if previewID == "" {
		return false
	}
	ps := h.extensions
	ps.ops.mu.Lock()
	defer ps.ops.mu.Unlock()
	for _, op := range ps.ops.byID {
		if op.claimed == previewID && op.ofMarket {
			return true
		}
	}
	return false
}

// opForInstall is the operation an install of preview pv goes on with: the one that made the preview, or a new one
// whose first steps are done already (a preview made before operations, or by the admin UI). It fails while another
// install goes on with the preview's operation.
func (h *Manager) opForInstall(pv *extension.Preview, previewID, actor string, l i18n.Lang) (*extOp, error) {
	if op, err := h.claimOpOfPreview(previewID); op != nil || err != nil {
		return op, err
	}
	kind, id := opInstall, ""
	if pv != nil {
		id = pv.Manifest.ID
		if pv.Upgrade {
			kind = opUpgrade
		}
	}
	// Preserve market ownership after a market install has consumed the preview.
	op := h.startOp(kind, id, actor, l, (pv != nil && pv.Market != "") || (pv == nil && h.claimedByMarket(previewID)))
	h.update(op, func(v *ExtensionOperation) {
		for _, s := range []string{extension.StepDownload, extension.StepVerify, extension.StepUnpack, extension.StepCheck} {
			setStep(v, s, extension.StepSkipped)
		}
		if pv != nil {
			v.Name, v.Version = pv.Manifest.Name, pv.Manifest.Version
			if pv.Market != "" {
				v.Market, v.Source = pv.Market, extension.MarketOrigin(pv.Market, pv.Manifest.ID)
			}
		}
	})
	return op, nil
}

// installing marks op as installing.
func (h *Manager) installing(op *extOp) {
	h.update(op, func(v *ExtensionOperation) {
		v.State, v.PreviewID = opRunning, ""
		setStep(v, extension.StepConsent, extension.StepDone)
		setStep(v, extension.StepTools, extension.StepRunning) // the registry says done or skipped, then install runs
	})
}
