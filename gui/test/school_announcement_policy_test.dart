import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sidravia_gui/application/gui_capabilities.dart';
import 'package:sidravia_gui/application/gui_connection_state.dart';
import 'package:sidravia_gui/application/gui_snapshot.dart';
import 'package:sidravia_gui/features/announcements/school_announcement_policy.dart';
import 'package:sidravia_gui/ipc/ipc_models.dart';
import 'package:sidravia_gui/shared/theme/appearance.dart';
import 'package:sidravia_gui/shared/theme/gui_settings.dart';

const school = InstitutionProfile(
  id: 'jlu',
  displayName: '吉林大学',
  protocolId: 'drcom',
  announcement: AnnouncementMetadata(
    url: 'http://login.jlu.edu.cn/notice_win.php',
    clientWidth: 600,
    clientHeight: 460,
  ),
);
const noMetadata = InstitutionProfile(
  id: 'jlu',
  displayName: '吉林大学',
  protocolId: 'drcom',
);
final started = DateTime(2026, 10, 10, 8);
final success = DateTime(2026, 10, 10, 9);
final now = DateTime(2026, 10, 10, 10);

void main() {
  test('resolver follows unique configured school independent of busy and credentials', () {
    final caps = capabilities(
      busy: true,
      sessions: [session(success)],
      configurations: [configuration(credentials: false)],
    );
    expect(caps.canConnect, false);
    expect(schoolAnnouncementProfileFor(caps), same(school));
    expect(schoolAnnouncementProfileFor(capabilities()), same(school));
    expect(
      schoolAnnouncementProfileFor(capabilities(profiles: [noMetadata])),
      isNull,
    );
    expect(schoolAnnouncementProfileFor(capabilities(profiles: [])), isNull);
    expect(
      schoolAnnouncementProfileFor(capabilities(profiles: [school, school])),
      isNull,
    );
    expect(
      schoolAnnouncementProfileFor(
        capabilities(configurations: [configuration(profileId: 'other')]),
      ),
      isNull,
    );
  });

  test('resolver rejects unavailable and ambiguous configuration/session relationships', () {
    for (final state in GuiConnectionState.values.where(
      (s) => s != GuiConnectionState.ready,
    )) {
      expect(schoolAnnouncementProfileFor(capabilities(state: state)), isNull);
    }
    expect(
      schoolAnnouncementProfileFor(
        const GuiCapabilities(
          state: GuiConnectionState.ready,
          snapshot: null,
          busy: false,
        ),
      ),
      isNull,
    );
    expect(
      schoolAnnouncementProfileFor(capabilities(daemonStatus: 'stopped')),
      isNull,
    );
    expect(
      schoolAnnouncementProfileFor(capabilities(configurations: [])),
      isNull,
    );
    expect(
      schoolAnnouncementProfileFor(
        capabilities(
          configurations: [
            configuration(),
            configuration(id: 'other'),
          ],
        ),
      ),
      isNull,
    );
    expect(
      schoolAnnouncementProfileFor(
        capabilities(sessions: [session(success, configurationId: 'wrong')]),
      ),
      isNull,
    );
    expect(
      schoolAnnouncementProfileFor(
        capabilities(
          sessions: [
            session(success),
            session(success, id: 'second'),
          ],
        ),
      ),
      isNull,
    );
  });

  test('first ready suppresses old connection but counts startup boundary and bootstrap autoLogin', () async {
    for (final at in [
      started.subtract(const Duration(seconds: 1)),
      started,
      success,
    ]) {
      final preferences = await owner();
      final policy = SchoolAnnouncementPolicy(startedAt: started);
      policy.consider(
        capabilities(state: GuiConnectionState.bootstrapping),
        preferences,
        now,
      );
      final result = policy.consider(
        capabilities(
          sessions: [session(at)],
          configurations: [configuration(autoLogin: true)],
        ),
        preferences,
        now,
      );
      expect(result, at.isBefore(started) ? isNull : same(school));
      expect(
        policy.consider(
          capabilities(sessions: [session(at, revision: 2)]),
          preferences,
          now,
        ),
        isNull,
      );
    }
  });

  test('initial authoritative empty snapshot allows later success even after clock adjustment', () async {
    final preferences = await owner();
    final policy = SchoolAnnouncementPolicy(startedAt: started);
    expect(policy.consider(capabilities(), preferences, now), isNull);
    expect(
      policy.consider(
        capabilities(
          sessions: [session(started.subtract(const Duration(days: 1)))],
        ),
        preferences,
        now,
      ),
      same(school),
    );
  });

  test(
    'duplicate older and newer revision with unchanged timestamp cannot replay',
    () async {
      final preferences = await owner();
      final policy = SchoolAnnouncementPolicy(startedAt: started);
      final original = capabilities(sessions: [session(success, revision: 5)]);
      expect(policy.consider(original, preferences, now), same(school));
      expect(policy.consider(original, preferences, now), isNull);
      expect(
        policy.consider(
          capabilities(
            sessions: [
              session(success.add(const Duration(hours: 1)), revision: 4),
            ],
          ),
          preferences,
          now,
        ),
        isNull,
      );
      expect(
        policy.consider(
          capabilities(sessions: [session(success, revision: 6)]),
          preferences,
          now,
        ),
        isNull,
      );
      expect(
        policy.consider(
          capabilities(sessions: [session(success.toUtc(), revision: 7)]),
          preferences,
          now,
        ),
        isNull,
      );
      expect(
        policy.consider(
          capabilities(
            sessions: [
              session(success.add(const Duration(minutes: 1)), revision: 8),
            ],
          ),
          preferences,
          now,
        ),
        same(school),
      );
    },
  );

  test('coalesced reconnect counts changed establishment without intermediate snapshot', () async {
    final preferences = await owner();
    final policy = SchoolAnnouncementPolicy(startedAt: started);
    expect(
      policy.consider(
        capabilities(sessions: [session(success)]),
        preferences,
        now,
      ),
      same(school),
    );
    expect(
      policy.consider(
        capabilities(
          sessions: [
            session(success.add(const Duration(minutes: 2)), revision: 2),
          ],
        ),
        preferences,
        now,
      ),
      same(school),
    );
    expect(preferences.lastAutomaticAnnouncementDate, '2026-10-10');
  });

  test(
    'waiting authenticating and missing timestamp never create successes',
    () async {
      final preferences = await owner();
      final policy = SchoolAnnouncementPolicy(startedAt: started);
      expect(
        policy.consider(
          capabilities(sessions: [session(null)]),
          preferences,
          now,
        ),
        isNull,
      );
      expect(
        policy.consider(
          capabilities(sessions: [session(success, revision: 2)]),
          preferences,
          now,
        ),
        same(school),
      );
      var revision = 3;
      for (final state in [
        SessionState.waitingForNetwork,
        SessionState.authenticating,
        SessionState.blockedByError,
        SessionState.authenticated,
      ]) {
        expect(
          policy.consider(
            capabilities(
              sessions: [session(success, state: state, revision: revision++)],
            ),
            preferences,
            now,
          ),
          isNull,
        );
      }
      expect(
        policy.consider(
          capabilities(sessions: [session(null, revision: revision++)]),
          preferences,
          now,
        ),
        isNull,
      );
      expect(
        policy.consider(
          capabilities(sessions: [session(success, revision: revision++)]),
          preferences,
          now,
        ),
        isNull,
      );
    },
  );

  test('stale retained snapshot and stopped daemon do not prune or reset observations', () async {
    final preferences = await owner();
    final policy = SchoolAnnouncementPolicy(startedAt: started);
    final authenticated = capabilities(sessions: [session(success)]);
    expect(policy.consider(authenticated, preferences, now), same(school));
    expect(
      policy.consider(
        capabilities(state: GuiConnectionState.stale, pid: 2),
        preferences,
        now,
      ),
      isNull,
    );
    expect(
      policy.consider(
        capabilities(pid: 2, daemonStatus: 'stopped'),
        preferences,
        now,
      ),
      isNull,
    );
    expect(policy.consider(authenticated, preferences, now), isNull);
  });

  test('daemon restart and replacement session are distinct; missing retained sessions prune', () async {
    final preferences = await owner();
    final policy = SchoolAnnouncementPolicy(startedAt: started);
    final first = capabilities(sessions: [session(success)]);
    expect(policy.consider(first, preferences, now), same(school));
    expect(
      policy.consider(
        capabilities(pid: 2, sessions: [session(success)]),
        preferences,
        now,
      ),
      same(school),
    );
    expect(
      policy.consider(
        capabilities(pid: 2, sessions: [session(success, id: 'replacement')]),
        preferences,
        now,
      ),
      same(school),
    );
    expect(policy.consider(capabilities(pid: 2), preferences, now), isNull);
    expect(
      policy.consider(
        capabilities(pid: 2, sessions: [session(success, id: 'replacement')]),
        preferences,
        now,
      ),
      same(school),
    );
  });

  test('ambiguous sessions and configurations consume all observations without later replay', () async {
    final preferences = await owner();
    final policy = SchoolAnnouncementPolicy(startedAt: started);
    expect(
      policy.consider(
        capabilities(
          sessions: [
            session(success),
            session(success, id: 'second'),
          ],
        ),
        preferences,
        now,
      ),
      isNull,
    );
    expect(
      policy.consider(
        capabilities(sessions: [session(success, revision: 2)]),
        preferences,
        now,
      ),
      isNull,
    );
    expect(
      policy.consider(
        capabilities(
          sessions: [
            session(success.add(const Duration(minutes: 1)), revision: 3),
          ],
          configurations: [
            configuration(),
            configuration(id: 'other'),
          ],
        ),
        preferences,
        now,
      ),
      isNull,
    );
    expect(
      policy.consider(
        capabilities(
          sessions: [
            session(success.add(const Duration(minutes: 1)), revision: 4),
          ],
        ),
        preferences,
        now,
      ),
      isNull,
    );
    expect(preferences.lastAutomaticAnnouncementDate, isNull);
  });

  test('optional retained school must match, but busy does not suppress real success', () async {
    final preferences = await owner();
    final policy = SchoolAnnouncementPolicy(startedAt: started);
    expect(
      policy.consider(
        capabilities(sessions: [session(success, profileId: 'other')]),
        preferences,
        now,
      ),
      isNull,
    );
    expect(
      policy.consider(
        capabilities(
          busy: true,
          sessions: [session(success, profileId: 'jlu', revision: 2)],
        ),
        preferences,
        now,
      ),
      isNull,
    );
    expect(
      policy.consider(
        capabilities(
          busy: true,
          sessions: [
            session(
              success.add(const Duration(minutes: 1)),
              profileId: 'jlu',
              revision: 3,
            ),
          ],
        ),
        preferences,
        now,
      ),
      same(school),
    );
  });

  for (final suppressed in ['manual', 'metadata']) {
    test(
      '$suppressed suppresses dispatch but consumes observations permanently',
      () async {
        final preferences = await owner(
          frequency: suppressed == 'manual'
              ? GuiAnnouncementFrequency.manualOnly
              : GuiAnnouncementFrequency.everyConnection,
        );
        final policy = SchoolAnnouncementPolicy(startedAt: started);
        final before = capabilities(
          sessions: [session(success)],
          profiles: suppressed == 'metadata' ? [noMetadata] : [school],
        );
        expect(policy.consider(before, preferences, now), isNull);
        expect(preferences.lastAutomaticAnnouncementDate, isNull);
        preferences.announcementFrequency =
            GuiAnnouncementFrequency.everyConnection;
        expect(
          policy.consider(
            capabilities(sessions: [session(success, revision: 2)]),
            preferences,
            now,
          ),
          isNull,
        );
        expect(schoolAnnouncementProfileFor(capabilities()), same(school));
        expect(
          preferences.lastAutomaticAnnouncementDate,
          isNull,
          reason: 'manual resolver never marks',
        );
        expect(
          policy.consider(
            capabilities(
              sessions: [
                session(success.add(const Duration(minutes: 1)), revision: 3),
              ],
            ),
            preferences,
            now,
          ),
          same(school),
        );
      },
    );
  }

  test('daily consumes current local trigger day, not establishment day, across midnight', () async {
    final preferences = await owner(
      frequency: GuiAnnouncementFrequency.dailyFirstConnection,
    );
    final policy = SchoolAnnouncementPolicy(startedAt: started);
    final late = DateTime(2026, 10, 10, 23, 59);
    final nextDay = DateTime(2026, 10, 11, 0, 1);
    expect(
      policy.consider(
        capabilities(sessions: [session(success)]),
        preferences,
        late,
      ),
      same(school),
    );
    expect(preferences.lastAutomaticAnnouncementDate, '2026-10-10');
    expect(
      policy.consider(
        capabilities(
          sessions: [
            session(success.add(const Duration(minutes: 1)), revision: 2),
          ],
        ),
        preferences,
        late,
      ),
      isNull,
    );
    expect(
      policy.consider(
        capabilities(
          sessions: [
            session(success.add(const Duration(minutes: 1)), revision: 3),
          ],
        ),
        preferences,
        nextDay,
      ),
      isNull,
      reason: 'midnight does not replay',
    );
    expect(
      policy.consider(
        capabilities(
          sessions: [
            session(success.add(const Duration(minutes: 2)), revision: 4),
          ],
        ),
        preferences,
        nextDay,
      ),
      same(school),
    );
    expect(preferences.lastAutomaticAnnouncementDate, '2026-10-11');
  });

  test('persisted daily marker suppresses fresh policy and option changes cannot replay', () async {
    final store = MemoryGuiSettingsStore()..settings = const GuiSettings();
    final first = await owner(
      store: store,
      frequency: GuiAnnouncementFrequency.dailyFirstConnection,
    );
    final policy = SchoolAnnouncementPolicy(startedAt: started);
    expect(
      policy.consider(capabilities(sessions: [session(success)]), first, now),
      same(school),
    );
    await first.settled;
    final second = Appearance(store: store);
    await second.initialize();
    addTearDown(second.dispose);
    final fresh = SchoolAnnouncementPolicy(startedAt: started);
    expect(
      fresh.consider(capabilities(sessions: [session(success)]), second, now),
      isNull,
    );
    second.announcementFrequency = GuiAnnouncementFrequency.everyConnection;
    expect(
      fresh.consider(
        capabilities(sessions: [session(success, revision: 2)]),
        second,
        now,
      ),
      isNull,
    );
    expect(
      fresh.consider(
        capabilities(
          sessions: [
            session(success.add(const Duration(minutes: 1)), revision: 3),
          ],
        ),
        second,
        now,
      ),
      same(school),
    );
    await second.settled;
  });

  test('marker is synchronous before return and owner reentrancy cannot duplicate intent', () async {
    final preferences = await owner();
    final policy = SchoolAnnouncementPolicy(startedAt: started);
    final caps = capabilities(sessions: [session(success)]);
    final nested = <InstitutionProfile?>[];
    preferences.addListener(() {
      expect(preferences.lastAutomaticAnnouncementDate, '2026-10-10');
      nested.add(policy.consider(caps, preferences, now));
    });
    expect(policy.consider(caps, preferences, now), same(school));
    expect(preferences.lastAutomaticAnnouncementDate, '2026-10-10');
    expect(nested, [null]);
    await preferences.settled;
    expect(nested.every((intent) => intent == null), true);
    expect(preferences.value, ThemeMode.dark);
    expect(preferences.diagnosticsExpanded, true);
    expect(preferences.closeBehavior, GuiCloseBehavior.exit);
    expect(preferences.startHidden, true);
    expect(
      preferences.hiddenAnnouncementBehavior,
      GuiHiddenAnnouncementBehavior.onRestore,
    );
    // Downstream failure has no completion callback to unmark or retry.
    expect(policy.consider(caps, preferences, now), isNull);
  });
}

Future<Appearance> owner({
  GuiAnnouncementFrequency frequency = GuiAnnouncementFrequency.everyConnection,
  MemoryGuiSettingsStore? store,
}) async {
  final backing =
      store ??
      (MemoryGuiSettingsStore()
        ..settings = GuiSettings(
          appearanceMode: GuiAppearanceMode.dark,
          diagnosticsExpanded: true,
          closeBehavior: GuiCloseBehavior.exit,
          startHidden: true,
          announcementFrequency: frequency,
          hiddenAnnouncementBehavior: GuiHiddenAnnouncementBehavior.onRestore,
        ));
  final preferences = Appearance(store: backing);
  await preferences.initialize();
  addTearDown(() async {
    await preferences.settled;
    preferences.dispose();
  });
  return preferences;
}

ConfigurationSummary configuration({
  String id = 'cfg',
  String profileId = 'jlu',
  bool credentials = false,
  bool autoLogin = false,
}) => ConfigurationSummary(
  id: id,
  displayName: '校园',
  institutionProfileId: profileId,
  institutionDisplayName: '吉林大学',
  authenticationProtocolId: 'drcom',
  username: 'account',
  credentialStored: credentials,
  storageProtection: 'protected',
  autoLogin: autoLogin,
);

SessionSummary session(
  DateTime? at, {
  String id = 'session',
  int revision = 1,
  String configurationId = 'cfg',
  String? profileId,
  SessionState state = SessionState.authenticated,
}) => SessionSummary(
  id: id,
  displayName: '',
  accountName: 'account',
  configurationId: configurationId,
  institutionProfileId: profileId,
  state: state,
  intent: SessionIntent.maintainAuthentication,
  revision: BigInt.from(revision),
  authenticationEstablishedAt: at,
  lastAuthenticationFailure: const SessionAuthenticationFailure(
    code: 'previous_failure',
    description: 'Retained historical failure',
    handlingRecommendation: 'Check details',
  ),
);

GuiCapabilities capabilities({
  List<SessionSummary> sessions = const [],
  List<InstitutionProfile> profiles = const [school],
  List<ConfigurationSummary>? configurations,
  GuiConnectionState state = GuiConnectionState.ready,
  int pid = 1,
  String daemonStatus = 'running',
  bool busy = false,
}) => GuiCapabilities(
  state: state,
  busy: busy,
  snapshot: GuiSnapshot(
    daemon: DaemonStatus(
      productVersion: 'test',
      buildId: 'test',
      pid: pid,
      status: daemonStatus,
      mode: 'desktop',
    ),
    profiles: profiles,
    configurations: configurations ?? [configuration()],
    sessions: sessions,
  ),
);
