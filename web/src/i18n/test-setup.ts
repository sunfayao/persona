// Vitest: every test file runs with the zh-Hans catalog loaded, as the page does before its modules load. The admin
// UI's namespaces too, for the admin tests.
import { adminCatalog } from "../admin/catalog";
import { initI18n } from "./index";

await initI18n("zh-Hans", adminCatalog);

// jsdom lacks Web Animations API (`Element.prototype.getAnimations`), which `@ui-sdk/core` presence/panel exit helpers call.
if (typeof Element !== "undefined" && typeof Element.prototype.getAnimations !== "function") {
  Element.prototype.getAnimations = () => [];
}
