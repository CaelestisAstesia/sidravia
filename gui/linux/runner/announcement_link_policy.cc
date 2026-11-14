#include "announcement_link_policy.h"
#include <glib.h>

namespace {
GUri* Parse(const std::string& text) {
  if (text.empty() || text.size() > 8192 ||
      !g_utf8_validate(text.data(), text.size(), nullptr)) return nullptr;
  for (const char* p = text.data(); p < text.data() + text.size(); p = g_utf8_next_char(p)) {
    const auto c = g_utf8_get_char(p);
    if (c <= 0x20 || (c >= 0x7f && c <= 0x9f) || g_unichar_isspace(c) || c == '"') return nullptr;
  }
  GUri* uri = g_uri_parse(text.c_str(), G_URI_FLAGS_ENCODED, nullptr);
  if (!uri) return nullptr;
  const char* scheme = g_uri_get_scheme(uri);
  if (!scheme || (g_ascii_strcasecmp(scheme,"http") && g_ascii_strcasecmp(scheme,"https")) ||
      !g_uri_get_host(uri) || !*g_uri_get_host(uri) || g_uri_get_userinfo(uri)) {
    g_uri_unref(uri); return nullptr;
  }
  return uri;
}
std::string Document(GUri* uri) {
  if (!uri) return {};
  g_autofree char* scheme = g_ascii_strdown(g_uri_get_scheme(uri), -1);
  g_autofree char* host = g_ascii_strdown(g_uri_get_host(uri), -1);
  auto port = g_uri_get_port(uri);
  if ((port == 80 && !g_strcmp0(scheme,"http")) || (port == 443 && !g_strcmp0(scheme,"https"))) port = -1;
  const auto* path = g_uri_get_path(uri);
  g_autofree char* value = g_uri_join(G_URI_FLAGS_ENCODED,scheme,nullptr,host,port,
      path && *path ? path : "/",g_uri_get_query(uri),nullptr);
  return value ? value : "";
}
}
bool AnnouncementHttpUri(const std::string& text) {
  g_autoptr(GUri) uri = Parse(text);
  return uri != nullptr;
}
AnnouncementLinkAction DecideAnnouncementLink(
    const std::string& document, const std::string& target, bool user,
    bool redirected, bool popup, bool ready) {
  g_autoptr(GUri) uri = Parse(target);
  g_autoptr(GUri) current = Parse(document);
  return DecideValidatedAnnouncementLink(uri != nullptr,
      current && uri && Document(current) == Document(uri),user,redirected,popup,ready);
}
