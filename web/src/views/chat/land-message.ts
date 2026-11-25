// Search hits, deep links and forwarded originals page back only when someone asks to open a message.
import { t } from "../../i18n";
import { ApiError, fullItem } from "../../lib/api";
import { insideFold } from "../../lib/turns";
import { toast } from "../../ui/atoms";
import { openFoldRange } from "./expanded";
import type { Transcript } from "./useTranscript";

const LANDING_WAIT_MS = 5000;
const nextFrame = () => new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));

interface Landing {
  target: string;
  mid: string;
  sid: string;
  scope: string;
  fromLink: boolean;
  current: () => boolean;
  transcript: () => Transcript;
  seek: (item: string, quiet?: boolean) => void;
  loadEarlier: (stop: () => boolean) => Promise<unknown>;
}

export async function landMessage({ target: item, mid, sid, scope, fromLink, current, transcript, seek, loadEarlier }: Landing) {
  if (!current()) return;
  const open = (quiet = false) => requestAnimationFrame(() => {
    if (current()) seek(item, quiet);
  });
  const holds = () => transcript().items.some((i) => i.id === item);
  if (holds()) return void open(true);
  if (!transcript().hasMore) {
    try {
      // A folded reply is not on screen. Open the fold that holds the original.
      const whole = await fullItem(mid, sid, item);
      if (!current()) return;
      const fold = transcript().items.find((f) => insideFold(whole, f));
      if (!fold) {
        if (fromLink) toast(t("chat-chatview.message_further_up"));
        return;
      }
      openFoldRange(scope, fold.seq, fold.fold!.through_seq);
      await transcript().expandFold(fold);
      open();
    } catch (error) {
      if (!fromLink || !current()) return;
      const missing = error instanceof ApiError && error.status === 404;
      if (missing) toast(t("chat-chatview.search_not_found"));
    }
    return;
  }
  // Older pages may already be loading to fill the viewport. Wait, then give the target a bounded search back.
  try {
    for (let page = 0, until = performance.now() + LANDING_WAIT_MS; page < 3 && current() && !holds(); page++) {
      while ((transcript().loading || transcript().loadingEarlier) && performance.now() < until && current()) await nextFrame();
      if (current()) await loadEarlier(() => !current() || holds());
    }
    if (!current()) return;
    if (holds()) open();
    else toast(t("chat-chatview.message_further_up"));
  } catch {
    // the search back failed: nothing more to show than the message not being there
  }
}
