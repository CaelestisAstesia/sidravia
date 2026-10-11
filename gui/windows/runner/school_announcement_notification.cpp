#include "school_announcement_notification.h"

#include <notificationactivationcallback.h>
#include <roapi.h>
#include <wrl.h>
#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Data.Xml.Dom.h>
#include <winrt/Windows.UI.Notifications.h>

#include <cstdint>
#include <cwchar>
#include <limits>
#include <mutex>
#include <cstring>
#include <vector>

namespace toast_api = winrt::Windows::UI::Notifications;
namespace xml_api = winrt::Windows::Data::Xml::Dom;

namespace {
constexpr WPARAM kClicked = 1, kFailed = 2;
constexpr wchar_t kDisplayName[] = L"\u6821\u56ed\u7f51\u516c\u544a";
constexpr wchar_t kBody[] = L"\u70b9\u51fb\u67e5\u770b\u5b66\u6821\u516c\u544a";
constexpr wchar_t kRegistryPrefix[] = L"Software\\Classes\\AppUserModelId\\";

struct CallbackState {
  std::mutex mutex;
  HWND window = nullptr;
  bool active = true;
  uint32_t generation = 0;
  std::wstring token, aumid;
  uint32_t post_failed_generation = 0;
  std::optional<int32_t> error_code;
};

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

bool Advance(CallbackState& state) {
  if (state.generation == static_cast<uint32_t>(INT32_MAX)) {
    state.active = false;
    state.token.clear();
    return false;
  }
  ++state.generation;
  state.token.clear();
  state.error_code.reset();
  return true;
}

// Worker callbacks never capture the native owner or a channel. Post while
// holding the identity lock, so shutdown cannot release/reuse the HWND midway.
bool PostTerminal(const std::weak_ptr<CallbackState>& weak,
                  const std::wstring& token, const std::wstring& aumid,
                  uint32_t expected_generation, WPARAM action,
                  std::optional<int32_t> error_code = std::nullopt) noexcept {
  try {
    auto state = weak.lock();
    if (!state) return false;
    std::lock_guard<std::mutex> lock(state->mutex);
    if (!state->active || !state->window || state->token != token ||
        state->aumid != aumid || token.empty() ||
        (expected_generation != 0 && state->generation != expected_generation))
      return false;
    if (action == kFailed) state->error_code = error_code;
    if (!PostMessageW(state->window, SchoolAnnouncementNotification::kCallbackMessage,
        action, static_cast<LPARAM>(state->generation))) {
      state->post_failed_generation = state->generation;
      state->token.clear();
      return false;
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
      if (!app_id || !args || wcsnlen_s(args, 33) != 32 ||
          wcsnlen_s(app_id, 256) >= 256) return E_INVALIDARG;
      for (size_t i = 0; i < 32; ++i) {
        if (!((args[i] >= L'0' && args[i] <= L'9') ||
              (args[i] >= L'a' && args[i] <= L'f'))) return E_INVALIDARG;
      }
      PostTerminal(state_, args, app_id, 0, kClicked);
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

struct SchoolAnnouncementNotification::Impl {
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
  toast_api::ToastNotification toast{nullptr};
  winrt::event_token failed_event{};
  bool failed_event_registered = false;

  bool ClearToast() noexcept {
    bool okay = true;
    if (toast && failed_event_registered) {
      toast.Failed(failed_event);
      failed_event_registered = false;
    }
    try { if (toast && notifier) notifier.Hide(toast); }
    catch (...) { okay = false; }
    toast = nullptr;
    try {
      if (initialized) toast_api::ToastNotificationManager::History().Remove(
          L"school", L"sidravia", aumid);
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
    if (notifier.Setting() != toast_api::NotificationSetting::Enabled)
      winrt::throw_hresult(E_ACCESSDENIED);
    namespace_name = name;
    {
      std::lock_guard<std::mutex> lock(state->mutex);
      state->aumid = aumid;
    }
    initialized = true;
  }
};

SchoolAnnouncementNotification::SchoolAnnouncementNotification(HWND window)
    : impl_(std::make_unique<Impl>(window)) {}

SchoolAnnouncementNotification::~SchoolAnnouncementNotification() { Dispose(); }

SchoolAnnouncementNotification::Result SchoolAnnouncementNotification::Show(const flutter::EncodableMap& arguments) {
  auto& owner = *impl_;
  try {
  if (owner.disposed || arguments.size() != 2) return {"failed", std::nullopt};
  const auto token_it = arguments.find(flutter::EncodableValue("token"));
  const auto ns_it = arguments.find(flutter::EncodableValue("namespace"));
  if (token_it == arguments.end() || ns_it == arguments.end()) return {"failed", std::nullopt};
  const auto* token = std::get_if<std::string>(&token_it->second);
  const auto* name = std::get_if<std::string>(&ns_it->second);
  std::wstring folded;
  if (!token || !name || !ValidToken(*token) || !FoldNamespace(*name, &folded) ||
      (owner.initialized && folded != owner.namespace_name)) return {"failed", std::nullopt};
    if (!owner.initialized) {
      try { owner.Initialize(folded); }
      catch (...) {
        if (!owner.ReleaseRegistration()) { Dispose(); }
        throw;
      }
    }
    uint32_t generation = 0;
    {
      std::lock_guard<std::mutex> lock(owner.state->mutex);
      if (!owner.state->active || !Advance(*owner.state)) return {"failed", std::nullopt};
      generation = owner.state->generation;
    }
    if (!owner.ClearToast()) return {"failed", std::nullopt};
    // Re-check enablement on each submission; never override user policy.
    if (owner.notifier.Setting() != toast_api::NotificationSetting::Enabled) return {"failed", std::nullopt};
    const std::wstring wide_token(token->begin(), token->end());
    xml_api::XmlDocument document;
    document.LoadXml(L"<toast launch=\"" + wide_token +
        L"\"><visual><binding template=\"ToastGeneric\"><text>" + kDisplayName +
        L"</text><text>" + kBody + L"</text></binding></visual></toast>");
    owner.toast = toast_api::ToastNotification(document);
    owner.toast.Tag(L"school");
    owner.toast.Group(L"sidravia");
    const std::weak_ptr<CallbackState> weak = owner.state;
    const std::wstring app_id = owner.aumid;
    owner.failed_event = owner.toast.Failed(
      [weak, wide_token, app_id, generation](toast_api::ToastNotification const&,
                                            toast_api::ToastFailedEventArgs const& args) {
        std::optional<int32_t> error_code;
        try { error_code = static_cast<int32_t>(args.ErrorCode()); }
        catch (...) { /* Unknown failure remains generic. */ }
        PostTerminal(weak, wide_token, app_id, generation, kFailed, error_code);
      });
    owner.failed_event_registered = true;
    {
      std::lock_guard<std::mutex> lock(owner.state->mutex);
      owner.state->token = wide_token;
    }
    owner.notifier.Show(owner.toast);
    bool post_failed = false;
    {
      std::lock_guard<std::mutex> lock(owner.state->mutex);
      post_failed = owner.state->post_failed_generation == generation;
    }
    if (post_failed) { owner.ClearToast(); return {"failed", std::nullopt}; }
    return {"submitted", std::nullopt};
  } catch (winrt::hresult_error const& error) {
    {
      std::lock_guard<std::mutex> lock(owner.state->mutex);
      owner.state->token.clear();
      owner.state->error_code.reset();
    }
    owner.ClearToast();
    return {"failed", static_cast<int32_t>(error.code())};
  } catch (...) {
    {
      std::lock_guard<std::mutex> lock(owner.state->mutex);
      owner.state->token.clear();
      owner.state->error_code.reset();
    }
    owner.ClearToast();
    return {"failed", std::nullopt};
  }
}

bool SchoolAnnouncementNotification::Dismiss() noexcept {
  try {
    auto& owner = *impl_;
    if (owner.disposed) return true;
    {
      std::lock_guard<std::mutex> lock(owner.state->mutex);
      if (!Advance(*owner.state)) return false;
    }
    return owner.ClearToast();
  } catch (...) { return false; }
}

bool SchoolAnnouncementNotification::Dispose() noexcept {
  try {
    auto& owner = *impl_;
    if (owner.disposed) return true;
    owner.disposed = true;
    {
      std::lock_guard<std::mutex> lock(owner.state->mutex);
      owner.state->active = false;
      owner.state->window = nullptr;
      owner.state->token.clear();
    }
    const bool hidden = owner.ClearToast();
    const bool released = owner.ReleaseRegistration();
    return hidden && released;
  } catch (...) { return false; }
}

std::optional<SchoolAnnouncementNotification::Callback>
SchoolAnnouncementNotification::TakeCallback(WPARAM action, LPARAM generation) noexcept {
  try {
    auto state = impl_->state;
    std::lock_guard<std::mutex> lock(state->mutex);
    if (!state->active || state->token.empty() || generation <= 0 ||
        static_cast<uint64_t>(generation) != state->generation ||
        (action != kClicked && action != kFailed)) return std::nullopt;
    std::string token;
    token.reserve(state->token.size());
    for (const wchar_t ch : state->token) {
      token.push_back(static_cast<char>(ch));
    }
    const auto error_code = action == kFailed ? state->error_code : std::nullopt;
    state->token.clear();
    state->error_code.reset();
    return Callback{action == kClicked ? "clicked" : "failed", token, error_code};
  } catch (...) { return std::nullopt; }
}
