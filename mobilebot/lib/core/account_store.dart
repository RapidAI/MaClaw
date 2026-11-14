import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// One signed-in account: the Hub it resolved to plus the enrollment
/// credentials `verify-and-start` returned. Multiple accounts are kept so the
/// user can switch without logging in again.
class Account {
  /// Stable key: `phone:<digits>` or the email, whichever identity enrolled.
  final String key;
  final String label;
  final String hubUrl;
  final String hubId;
  final String hubCenterUrl;
  final String tenantId;
  final String tenantName;
  final String userId;
  final String email;
  final String phoneNumber;

  /// Bearer token for every Hub API (`viewer_token`/`access_token`).
  final String token;
  final String machineId;
  final String machineToken;
  final String clientId;
  final DateTime addedAt;

  const Account({
    required this.key,
    required this.label,
    required this.hubUrl,
    required this.token,
    this.hubId = '',
    this.hubCenterUrl = '',
    this.tenantId = '',
    this.tenantName = '',
    this.userId = '',
    this.email = '',
    this.phoneNumber = '',
    this.machineId = '',
    this.machineToken = '',
    this.clientId = '',
    required this.addedAt,
  });

  String get subtitle {
    if (tenantName.isNotEmpty) return tenantName;
    final host = Uri.tryParse(hubUrl)?.host ?? hubUrl;
    return host;
  }

  factory Account.fromJson(Map<String, dynamic> json) => Account(
        key: json['key'] as String? ?? '',
        label: json['label'] as String? ?? '',
        hubUrl: json['hub_url'] as String? ?? '',
        hubId: json['hub_id'] as String? ?? '',
        hubCenterUrl: json['hubcenter_url'] as String? ?? '',
        tenantId: json['tenant_id'] as String? ?? '',
        tenantName: json['tenant_name'] as String? ?? '',
        userId: json['user_id'] as String? ?? '',
        email: json['email'] as String? ?? '',
        phoneNumber: json['phone_number'] as String? ?? '',
        token: json['token'] as String? ?? '',
        machineId: json['machine_id'] as String? ?? '',
        machineToken: json['machine_token'] as String? ?? '',
        clientId: json['client_id'] as String? ?? '',
        addedAt:
            DateTime.tryParse(json['added_at'] as String? ?? '') ??
                DateTime.fromMillisecondsSinceEpoch(0),
      );

  Map<String, dynamic> toJson() => {
        'key': key,
        'label': label,
        'hub_url': hubUrl,
        'hub_id': hubId,
        'hubcenter_url': hubCenterUrl,
        'tenant_id': tenantId,
        'tenant_name': tenantName,
        'user_id': userId,
        'email': email,
        'phone_number': phoneNumber,
        'token': token,
        'machine_id': machineId,
        'machine_token': machineToken,
        'client_id': clientId,
        'added_at': addedAt.toIso8601String(),
      };
}

/// Multi-account store. Tokens live in secure storage; only the active key is
/// kept in shared preferences so the app cold-starts on the last account.
class AccountStore {
  static const _accountsPrefKey = 'mobilebot.accounts.v1';
  static const _activePrefKey = 'mobilebot.activeAccount';

  static const _storage = FlutterSecureStorage();

  List<Account> _accounts = const [];
  String? _activeKey;

  List<Account> get accounts => List.unmodifiable(_accounts);
  String? get activeKey => _activeKey;

  Account? get active {
    for (final account in _accounts) {
      if (account.key == _activeKey) return account;
    }
    return null;
  }

  Future<void> load() async {
    final raw = await _storage.read(key: _accountsPrefKey);
    if (raw != null && raw.isNotEmpty) {
      try {
        final list = jsonDecode(raw);
        if (list is List) {
          _accounts = list
              .whereType<Map>()
              .map((e) => Account.fromJson(Map<String, dynamic>.from(e)))
              .where((a) => a.key.isNotEmpty && a.token.isNotEmpty)
              .toList(growable: false);
        }
      } on FormatException {
        _accounts = const [];
      }
    }
    final prefs = await SharedPreferences.getInstance();
    _activeKey = prefs.getString(_activePrefKey);
    if (_activeKey == null || active == null) {
      _activeKey = _accounts.isNotEmpty ? _accounts.first.key : null;
    }
  }

  Future<void> _persist() async {
    await _storage.write(
      key: _accountsPrefKey,
      value: jsonEncode([for (final a in _accounts) a.toJson()]),
    );
    final prefs = await SharedPreferences.getInstance();
    final active = _activeKey;
    if (active != null) {
      await prefs.setString(_activePrefKey, active);
    } else {
      await prefs.remove(_activePrefKey);
    }
  }

  /// Adds or replaces the account with the same key, then activates it.
  Future<Account> upsert(Account account) async {
    final next = [
      account,
      ..._accounts.where((a) => a.key != account.key),
    ];
    _accounts = next;
    _activeKey = account.key;
    await _persist();
    return account;
  }

  Future<void> setActive(String key) async {
    if (!_accounts.any((a) => a.key == key)) return;
    _activeKey = key;
    await _persist();
  }

  /// Removes an account. When it was active the next stored account becomes
  /// active; with none left the app returns to the login gate.
  Future<void> remove(String key) async {
    _accounts = _accounts.where((a) => a.key != key).toList(growable: false);
    if (_activeKey == key) {
      _activeKey = _accounts.isNotEmpty ? _accounts.first.key : null;
    }
    await _persist();
  }
}
