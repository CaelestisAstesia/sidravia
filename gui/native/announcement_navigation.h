#ifndef SIDRAVIA_ANNOUNCEMENT_NAVIGATION_H_
#define SIDRAVIA_ANNOUNCEMENT_NAVIGATION_H_

enum class AnnouncementLinkAction { in_window, external_browser, block };

// Native adapters own URL validation/canonicalization; this is the shared
// user-intent contract for main navigation and new-window requests.
inline AnnouncementLinkAction DecideValidatedAnnouncementLink(
    bool valid_target, bool same_document, bool user_initiated,
    bool redirected, bool new_window, bool document_ready) {
  if (!valid_target || (new_window && (!user_initiated || !document_ready))) {
    return AnnouncementLinkAction::block;
  }
  if (!user_initiated || redirected || !document_ready ||
      (!new_window && same_document)) return AnnouncementLinkAction::in_window;
  return AnnouncementLinkAction::external_browser;
}
#endif
