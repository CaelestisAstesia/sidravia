#include "system_notification.h"

#include <notificationactivationcallback.h>
#include <roapi.h>
#include <wrl.h>
#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Foundation.Collections.h>
#include <winrt/Windows.Data.Xml.Dom.h>
#include <winrt/Windows.UI.Notifications.h>

#include <chrono>
#include <cstdint>
#include <cwchar>
#include <limits>
#include <mutex>
#include <map>
#include <set>
#include <cstring>
#include <vector>

namespace toast_api = winrt::Windows::UI::Notifications;
namespace xml_api = winrt::Windows::Data::Xml::Dom;

namespace {
constexpr WPARAM kClicked = 1, kFailed = 2;
constexpr wchar_t kDisplayName[] = L"Sidravia";

constexpr wchar_t kRegistryPrefix[] = L"Software\\Classes\\AppUserModelId\\";

struct Pending {
  std::wstring token;
  std::string category;
  std::optional<int32_t> error_code;
};
struct CallbackState {
  std::mutex mutex;
  HWND window = nullptr;
  bool active = true;
  uint32_t generation = 0;
  std::wstring aumid;
  std::map<uint32_t, Pending> pending;
  std::set<uint32_t> post_failed;
};
bool ValidCategory(const std::string& category) {
  return category == "announcement" || category == "connection";
}
std::wstring Text(const std::string& value, size_t limit) {
  if (value.empty() || value.size() > limit * 4 || value.find('\0') != std::string::npos)
    winrt::throw_hresult(E_INVALIDARG);
  const int size = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS,
      value.data(), static_cast<int>(value.size()), nullptr, 0);
  if (size <= 0 || static_cast<size_t>(size) > limit) winrt::throw_hresult(E_INVALIDARG);
  std::wstring result(size, L'\0');
  if (MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, value.data(),
      static_cast<int>(value.size()), result.data(), size) != size)
    winrt::throw_hresult(E_INVALIDARG);
  return result;
}

bool ValidToken(const std::string& token) {
  if (token.size() != 32) return false;
  for (const unsigned char ch : token) {
    if (!((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f'))) return false;
  }
  return true;
}

bool FoldNamespace(const std::string& name, std::wstring* folded) {
  folded->clear();
  if (name.empty()) return true;
  if (name.size() > 64 || name.front() == '.' || name.back() == '.' ||
      name.find("..") != std::string::npos) return false;
  for (const unsigned char ch : name) {
    if (!((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') ||
          (ch >= '0' && ch <= '9') || ch == '.' || ch == '_' || ch == '-'))
      return false;
    folded->push_back(static_cast<wchar_t>(
        ch >= 'A' && ch <= 'Z' ? ch + ('a' - 'A') : ch));
  }
  return *folded != L"production" && *folded != L"default" && *folded != L"prod";
}

// Worker callbacks hold only shared identities and post scalar messages.
bool PostTerminal(const std::weak_ptr<CallbackState>& weak,
                  const std::wstring& token, const std::wstring& aumid,
                  uint32_t expected_generation, WPARAM action,
                  std::optional<int32_t> error_code = std::nullopt,
                  const std::string& category = "") noexcept {
  try {
    auto state = weak.lock();
    if (!state) return false;
    std::lock_guard<std::mutex> lock(state->mutex);
    if (!state->active || !state->window || state->aumid != aumid || token.empty()) return false;
    auto it = state->pending.end();
    if (expected_generation) it = state->pending.find(expected_generation);
    else for (auto current = state->pending.begin(); current != state->pending.end(); ++current) {
      if (current->second.token == token && current->second.category == category) { it = current; break; }
    }
    if (it == state->pending.end() || it->second.token != token) return false;
    if (action == kFailed) it->second.error_code = error_code;
    if (!PostMessageW(state->window, SystemNotification::kCallbackMessage, action,
        static_cast<LPARAM>(it->first))) {
      state->post_failed.insert(it->first); state->pending.erase(it); return false;
    }
    return true;
  } catch (...) { return false; }
}

class ActivationCallback final : public Microsoft::WRL::RuntimeClass<
    Microsoft::WRL::RuntimeClassFlags<Microsoft::WRL::ClassicCom>,
    INotificationActivationCallback> {
 public:
  explicit ActivationCallback(std::weak_ptr<CallbackState> state)
      : state_(std::move(state)) {}
  HRESULT STDMETHODCALLTYPE Activate(LPCWSTR app_id, LPCWSTR args,
      const NOTIFICATION_USER_INPUT_DATA*, ULONG) override {
    try {
      if (!app_id || !args || wcsnlen_s(args, 50) >= 50 ||
          wcsnlen_s(app_id, 256) >= 256) return E_INVALIDARG;
      const std::wstring launch(args);
      const auto separator = launch.find(L':');
      if (separator == std::wstring::npos) return E_INVALIDARG;
      const auto kind = launch.substr(0, separator);
      if (kind != L"announcement" && kind != L"connection") return E_INVALIDARG;
      const auto token = launch.substr(separator + 1);
      if (token.size() != 32) return E_INVALIDARG;
      for (const auto ch : token) {
        if (!((ch >= L'0' && ch <= L'9') || (ch >= L'a' && ch <= L'f'))) return E_INVALIDARG;
      }
      PostTerminal(state_, token, app_id, 0, kClicked, std::nullopt,
          kind == L"announcement" ? "announcement" : "connection");
      return S_OK;
    } catch (...) { return E_FAIL; }
  }
 private:
  std::weak_ptr<CallbackState> state_;
};

class ActivationFactory final : public Microsoft::WRL::RuntimeClass<
    Microsoft::WRL::RuntimeClassFlags<Microsoft::WRL::ClassicCom>, IClassFactory> {
 public:
  explicit ActivationFactory(std::weak_ptr<CallbackState> state)
      : state_(std::move(state)) {}
  HRESULT STDMETHODCALLTYPE CreateInstance(IUnknown* outer, REFIID iid,
                                            void** object) override {
    if (!object) return E_POINTER;
    *object = nullptr;
    if (outer) return CLASS_E_NOAGGREGATION;
    auto callback = Microsoft::WRL::Make<ActivationCallback>(state_);
    return callback ? callback.CopyTo(iid, object) : E_OUTOFMEMORY;
  }
  HRESULT STDMETHODCALLTYPE LockServer(BOOL) override { return S_OK; }
 private:
  std::weak_ptr<CallbackState> state_;
};

struct RegistryValue {
  bool present = false;
  DWORD type = REG_NONE;
  std::vector<BYTE> bytes;
};

RegistryValue ReadValue(HKEY key, const wchar_t* name) {
  RegistryValue value;
  DWORD bytes = 0;
  LSTATUS result = RegQueryValueExW(key, name, nullptr, &value.type, nullptr, &bytes);
  if (result == ERROR_FILE_NOT_FOUND) return value;
  winrt::check_win32(result);
  value.bytes.resize(bytes);
  winrt::check_win32(RegQueryValueExW(key, name, nullptr, &value.type,
                                    value.bytes.data(), &bytes));
  value.present = true;
  return value;
}

void WriteString(HKEY key, const wchar_t* name, const std::wstring& text) {
  const size_t bytes = (text.size() + 1) * sizeof(wchar_t);
  if (bytes > MAXDWORD) winrt::throw_hresult(E_INVALIDARG);
  winrt::check_win32(RegSetValueExW(key, name, 0, REG_SZ,
      reinterpret_cast<const BYTE*>(text.c_str()), static_cast<DWORD>(bytes)));
}

bool RestoreValue(HKEY key, const wchar_t* name, const RegistryValue& old,
                   const std::wstring& written) noexcept {
  try {
    const auto current = ReadValue(key, name);
    const size_t expected_size = (written.size() + 1) * sizeof(wchar_t);
    if (!current.present || current.type != REG_SZ ||
        current.bytes.size() != expected_size ||
        memcmp(current.bytes.data(), written.c_str(), expected_size) != 0) return true;
    const LSTATUS result = old.present
        ? RegSetValueExW(key, name, 0, old.type, old.bytes.data(),
                        static_cast<DWORD>(old.bytes.size()))
        : RegDeleteValueW(key, name);
    return result == ERROR_SUCCESS || result == ERROR_FILE_NOT_FOUND;
  } catch (...) { return false; }
}
}  // namespace

struct SystemNotification::Impl {
  explicit Impl(HWND window) : state(std::make_shared<CallbackState>()) {
    state->window = window;
  }
  std::shared_ptr<CallbackState> state;
  bool initialized = false, disposed = false, ro_initialized = false;
  DWORD cookie = 0;
  HKEY registry = nullptr;
  RegistryValue old_display, old_activator;
  bool display_written = false, activator_written = false;
  std::wstring namespace_name, aumid, clsid_text;
  toast_api::ToastNotifier notifier{nullptr};
  struct Record {
    toast_api::ToastNotification toast{nullptr};
    winrt::event_token failed_event{};
    bool failed_event_registered = false;
    uint32_t generation = 0;
  };
  std::map<std::string, Record> records;

  bool Clear(const std::string& category) noexcept {
    bool okay = true;
    const auto it = records.find(category);
    if (it != records.end()) {
      const auto generation = it->second.generation;
      {
        std::lock_guard<std::mutex> lock(state->mutex);
        state->pending.erase(generation); state->post_failed.erase(generation);
      }
      try { if (it->second.failed_event_registered) it->second.toast.Failed(it->second.failed_event); }
      catch (...) { okay = false; }
      try { if (notifier && it->second.toast) notifier.Hide(it->second.toast); }
      catch (...) { okay = false; }
      records.erase(it);
    }
    try {
      if (initialized) toast_api::ToastNotificationManager::History().Remove(
          std::wstring(category.begin(), category.end()), L"sidravia", aumid);
    } catch (...) { okay = false; }
    return okay;
  }

  bool ReleaseRegistration() noexcept {
    bool okay = true;
    notifier = nullptr;
    if (cookie) {
      const HRESULT revoked = CoRevokeClassObject(cookie);
      if (SUCCEEDED(revoked)) cookie = 0;
      else okay = false;
    }
    if (registry) {
      if (activator_written && !RestoreValue(registry, L"CustomActivator", old_activator, clsid_text)) okay = false;
      if (display_written && !RestoreValue(registry, L"DisplayName", old_display, kDisplayName)) okay = false;
      if (RegCloseKey(registry) != ERROR_SUCCESS) okay = false;
      registry = nullptr;
    }
    display_written = activator_written = false;
    initialized = false;
    if (ro_initialized) { RoUninitialize(); ro_initialized = false; }
    return okay;
  }

  toast_api::NotificationSetting ReadInitialSetting() {
    try {
      return notifier.Setting();
    } catch (winrt::hresult_error const& error) {
      if (error.code() != HRESULT_FROM_WIN32(ERROR_NOT_FOUND)) throw;
      // A new identityless AUMID has no notification record until its first
      // submission. Materialize it silently, then query the user's policy.
      xml_api::XmlDocument document;
      document.LoadXml(
          L"<toast><visual><binding template=\"ToastGeneric\">"
          L"<text>Sidravia</text></binding></visual></toast>");
      toast_api::ToastNotification bootstrap(document);
      bootstrap.SuppressPopup(true);
      bootstrap.Tag(L"identity-bootstrap");
      bootstrap.Group(L"sidravia");
      bootstrap.ExpirationTime(winrt::clock::now() + std::chrono::seconds(15));
      notifier.Show(bootstrap);
      toast_api::ToastNotificationManager::History().Remove(
          L"identity-bootstrap", L"sidravia", aumid);
      return notifier.Setting();
    }
  }

  void Initialize(const std::wstring& name) {
    const HRESULT ro = RoInitialize(RO_INIT_SINGLETHREADED);
    winrt::check_hresult(ro);
    ro_initialized = true;  // S_OK and S_FALSE both require balancing.
    GUID clsid{};
    winrt::check_hresult(CoCreateGuid(&clsid));
    wchar_t formatted[39]{};
    if (StringFromGUID2(clsid, formatted, 39) != 39) winrt::throw_hresult(E_FAIL);
    clsid_text = formatted;
    aumid = L"io.github.astesia.Sidravia.Announcements";
    if (!name.empty()) aumid += L".ns." + name;
    const std::wstring key = std::wstring(kRegistryPrefix) + aumid;
    winrt::check_win32(RegCreateKeyExW(HKEY_CURRENT_USER, key.c_str(), 0,
        nullptr, 0, KEY_QUERY_VALUE | KEY_SET_VALUE, nullptr, &registry, nullptr));
    old_display = ReadValue(registry, L"DisplayName");
    old_activator = ReadValue(registry, L"CustomActivator");
    WriteString(registry, L"DisplayName", kDisplayName);
    display_written = true;
    WriteString(registry, L"CustomActivator", clsid_text);
    activator_written = true;
    auto factory = Microsoft::WRL::Make<ActivationFactory>(state);
    if (!factory) winrt::throw_hresult(E_OUTOFMEMORY);
    winrt::check_hresult(CoRegisterClassObject(clsid, factory.Get(),
        CLSCTX_LOCAL_SERVER, REGCLS_MULTIPLEUSE, &cookie));
    notifier = toast_api::ToastNotificationManager::CreateToastNotifier(aumid);
    if (ReadInitialSetting() != toast_api::NotificationSetting::Enabled)
      winrt::throw_hresult(E_ACCESSDENIED);
    namespace_name = name;
    {
      std::lock_guard<std::mutex> lock(state->mutex);
      state->aumid = aumid;
    }
    initialized = true;
  }
};

SystemNotification::SystemNotification(HWND window)
    : impl_(std::make_unique<Impl>(window)) {}

SystemNotification::~SystemNotification() { Dispose(); }

SystemNotification::Result SystemNotification::Show(const flutter::EncodableMap& arguments) {
  auto& owner = *impl_;
  std::string category;
  try {
    if (owner.disposed || arguments.size() != 5) return {"failed", std::nullopt};
    auto field = [&](const char* key) -> const std::string* {
      const auto it = arguments.find(flutter::EncodableValue(key));
      return it == arguments.end() ? nullptr : std::get_if<std::string>(&it->second);
    };
    const auto token = field("token"), name = field("namespace"), kind = field("category"),
        title = field("title"), body = field("body");
    std::wstring folded;
    if (!token || !name || !kind || !title || !body || !ValidToken(*token) ||
        !ValidCategory(*kind) || !FoldNamespace(*name, &folded) ||
        (owner.initialized && folded != owner.namespace_name)) return {"failed", std::nullopt};
    const auto wide_title = Text(*title, 128), wide_body = Text(*body, 512);
    category = *kind;
    if (!owner.initialized) {
      try { owner.Initialize(folded); }
      catch (...) { if (!owner.ReleaseRegistration()) Dispose(); throw; }
    }
    if (!owner.Clear(category)) return {"failed", std::nullopt};
    if (owner.notifier.Setting() != toast_api::NotificationSetting::Enabled)
      return {"failed", std::nullopt};
    uint32_t generation = 0;
    {
      std::lock_guard<std::mutex> lock(owner.state->mutex);
      if (!owner.state->active || owner.state->generation == static_cast<uint32_t>(INT32_MAX))
        return {"failed", std::nullopt};
      generation = ++owner.state->generation;
    }
    const std::wstring wide_token(token->begin(), token->end());
    xml_api::XmlDocument document;
    document.LoadXml(L"<toast launch=\"" + std::wstring(category.begin(), category.end()) + L":" + wide_token +
        L"\"><visual><binding template=\"ToastGeneric\"><text/><text/></binding></visual></toast>");
    const auto text_nodes = document.SelectNodes(L"/toast/visual/binding/text");
    text_nodes.GetAt(0).InnerText(wide_title); text_nodes.GetAt(1).InnerText(wide_body);
    auto& record = owner.records[category];
    record.generation = generation;
    record.toast = toast_api::ToastNotification(document);
    record.toast.Tag(std::wstring(category.begin(), category.end()));
    record.toast.Group(L"sidravia");
    const std::weak_ptr<CallbackState> weak = owner.state;
    const std::wstring app_id = owner.aumid;
    record.failed_event = record.toast.Failed(
      [weak, wide_token, app_id, generation](toast_api::ToastNotification const&,
          toast_api::ToastFailedEventArgs const& args) {
        std::optional<int32_t> error;
        try { error = static_cast<int32_t>(args.ErrorCode()); } catch (...) {}
        PostTerminal(weak, wide_token, app_id, generation, kFailed, error);
      });
    record.failed_event_registered = true;
    {
      std::lock_guard<std::mutex> lock(owner.state->mutex);
      owner.state->pending.emplace(generation, Pending{wide_token, category, std::nullopt});
    }
    owner.notifier.Show(record.toast);
    bool post_failed;
    {
      std::lock_guard<std::mutex> lock(owner.state->mutex);
      post_failed = owner.state->post_failed.erase(generation) != 0;
    }
    if (post_failed) { owner.Clear(category); return {"failed", std::nullopt}; }
    return {"submitted", std::nullopt};
  } catch (winrt::hresult_error const& error) {
    if (!category.empty()) owner.Clear(category);
    return {"failed", static_cast<int32_t>(error.code())};
  } catch (...) {
    if (!category.empty()) owner.Clear(category);
    return {"failed", std::nullopt};
  }
}

bool SystemNotification::Dismiss(const std::string& category) noexcept {
  if (!ValidCategory(category)) return false;
  return impl_->disposed || impl_->Clear(category);
}
bool SystemNotification::Dispose() noexcept {
  auto& owner = *impl_;
  if (owner.disposed) return true;
  owner.disposed = true;
  {
    std::lock_guard<std::mutex> lock(owner.state->mutex);
    owner.state->active = false; owner.state->window = nullptr;
    owner.state->pending.clear(); owner.state->post_failed.clear();
  }
  const bool announcement = owner.Clear("announcement");
  const bool connection = owner.Clear("connection");
  const bool registration = owner.ReleaseRegistration();
  return announcement && connection && registration;
}
std::optional<SystemNotification::Callback>
SystemNotification::TakeCallback(WPARAM action, LPARAM generation) noexcept {
  try {
    auto state = impl_->state;
    std::lock_guard<std::mutex> lock(state->mutex);
    if (!state->active || generation <= 0 || static_cast<uint64_t>(generation) > INT32_MAX ||
        (action != kClicked && action != kFailed)) return std::nullopt;
    const auto it = state->pending.find(static_cast<uint32_t>(generation));
    if (it == state->pending.end()) return std::nullopt;
    const auto pending = it->second; state->pending.erase(it);
    std::string token;
    token.reserve(pending.token.size());
    for (const auto ch : pending.token) token.push_back(static_cast<char>(ch));
    return Callback{action == kClicked ? "clicked" : "failed",
        token, pending.category, pending.error_code};
  } catch (...) { return std::nullopt; }
}
