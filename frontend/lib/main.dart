import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import 'package:web_socket_channel/web_socket_channel.dart';

void main() {
  runApp(const RvolAlertApp());
}

class RvolAlertApp extends StatelessWidget {
  const RvolAlertApp({super.key, this.enableNetwork = true});

  final bool enableNetwork;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'RVOL Alert',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(useMaterial3: true, colorSchemeSeed: Colors.blue),
      home: HomePage(enableNetwork: enableNetwork),
    );
  }
}

class HomePage extends StatefulWidget {
  const HomePage({super.key, this.enableNetwork = true});

  final bool enableNetwork;

  @override
  State<HomePage> createState() => _HomePageState();
}

class _HomePageState extends State<HomePage> {
  static const String apiBaseUrl = 'http://localhost:8080';
  static const String wsUrl = 'ws://localhost:8080/ws';

  // ============================================================
  // 页面
  // ============================================================

  int selectedTab = 0;

  // ============================================================
  // Alert
  // ============================================================

  List<Map<String, dynamic>> alertBatches = [];

  bool wsConnected = false;
  String? wsError;

  WebSocketChannel? channel;
  StreamSubscription? wsSubscription;
  Timer? reconnectTimer;

  // ============================================================
  // Ranking
  // ============================================================

  List<Map<String, dynamic>> ranking = [];

  bool rankingLoading = false;
  String? rankingError;

  Timer? rankingRefreshTimer;

  // ============================================================
  // 生命周期
  // ============================================================

  @override
  void initState() {
    super.initState();

    if (!widget.enableNetwork) {
      return;
    }

    connectWebSocket();
    loadRanking();

    rankingRefreshTimer = Timer.periodic(const Duration(seconds: 60), (_) {
      loadRanking();
    });
  }

  @override
  void dispose() {
    reconnectTimer?.cancel();
    rankingRefreshTimer?.cancel();

    wsSubscription?.cancel();
    channel?.sink.close();

    super.dispose();
  }

  // ============================================================
  // WebSocket
  // ============================================================

  void connectWebSocket() {
    reconnectTimer?.cancel();

    try {
      final newChannel = WebSocketChannel.connect(Uri.parse(wsUrl));

      channel = newChannel;

      wsSubscription = newChannel.stream.listen(
        (message) {
          handleWebSocketMessage(message);
        },
        onDone: () {
          handleWebSocketDisconnected();
        },
        onError: (error) {
          handleWebSocketDisconnected(error: error.toString());
        },
        cancelOnError: true,
      );

      if (!mounted) {
        return;
      }

      setState(() {
        wsConnected = true;
        wsError = null;
      });
    } catch (e) {
      handleWebSocketDisconnected(error: e.toString());
    }
  }

  void handleWebSocketDisconnected({String? error}) {
    if (!mounted) {
      return;
    }

    setState(() {
      wsConnected = false;

      if (error != null) {
        wsError = error;
      }
    });

    reconnectTimer?.cancel();

    reconnectTimer = Timer(const Duration(seconds: 3), () {
      if (mounted) {
        connectWebSocket();
      }
    });
  }

  void handleWebSocketMessage(dynamic message) {
    try {
      final decoded = jsonDecode(message as String);

      if (decoded is! Map<String, dynamic>) {
        return;
      }

      final dynamic rawBatches = decoded['batches'];

      if (rawBatches is! List) {
        return;
      }

      final newBatches = rawBatches
          .whereType<Map>()
          .map((item) => Map<String, dynamic>.from(item))
          .toList();

      if (!mounted) {
        return;
      }

      setState(() {
        alertBatches = newBatches;
        wsConnected = true;
        wsError = null;
      });
    } catch (e) {
      if (!mounted) {
        return;
      }

      setState(() {
        wsError = 'WebSocket 数据解析失败: $e';
      });
    }
  }

  void reconnectWebSocket() {
    reconnectTimer?.cancel();

    wsSubscription?.cancel();
    channel?.sink.close();

    channel = null;
    wsSubscription = null;

    connectWebSocket();
  }

  // ============================================================
  // Ranking HTTP
  // ============================================================

  Future<void> loadRanking() async {
    if (!mounted) {
      return;
    }

    setState(() {
      rankingLoading = true;
      rankingError = null;
    });

    try {
      final response = await http.get(
        Uri.parse('$apiBaseUrl/ranking?limit=100'),
      );

      if (response.statusCode != 200) {
        throw Exception('HTTP ${response.statusCode}');
      }

      final data = jsonDecode(response.body);

      if (data is! Map<String, dynamic>) {
        throw Exception('返回数据格式错误');
      }

      final dynamic rawRanking = data['ranking'];

      if (rawRanking is! List) {
        throw Exception('ranking 字段格式错误');
      }

      final newRanking = rawRanking
          .whereType<Map>()
          .map((item) => Map<String, dynamic>.from(item))
          .toList();

      if (!mounted) {
        return;
      }

      setState(() {
        ranking = newRanking;
        rankingLoading = false;
      });
    } catch (e) {
      if (!mounted) {
        return;
      }

      setState(() {
        rankingLoading = false;
        rankingError = e.toString();
      });
    }
  }

  // ============================================================
  // 主 UI
  // ============================================================

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text(
          'RVOL Alert',
          style: TextStyle(fontWeight: FontWeight.w700),
        ),
        actions: [buildConnectionStatus(), const SizedBox(width: 12)],
      ),
      body: Column(
        children: [
          Expanded(
            child: selectedTab == 0 ? buildAlertsPage() : buildRankingPage(),
          ),
          buildNavigationBar(),
        ],
      ),
    );
  }

  Widget buildConnectionStatus() {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Container(
          width: 8,
          height: 8,
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            color: wsConnected ? Colors.green : Colors.red,
          ),
        ),
        const SizedBox(width: 7),
        Text(
          wsConnected ? '实时连接' : '连接断开',
          style: const TextStyle(fontSize: 13),
        ),
      ],
    );
  }

  // ============================================================
  // Alert 页面
  // ============================================================

  Widget buildAlertsPage() {
    final visibleBatches = alertBatches.where((batch) {
      final alerts = batch['alerts'];

      return alerts is List && alerts.isNotEmpty;
    }).toList();

    if (visibleBatches.isEmpty) {
      return RefreshIndicator(
        onRefresh: () async {
          reconnectWebSocket();
        },
        child: ListView(
          physics: const AlwaysScrollableScrollPhysics(),
          children: [
            const SizedBox(height: 120),
            Center(
              child: Column(
                children: [
                  Icon(
                    Icons.notifications_none,
                    size: 64,
                    color: Colors.grey.shade500,
                  ),
                  const SizedBox(height: 16),
                  const Text(
                    '当前没有 Alert',
                    style: TextStyle(fontSize: 21, fontWeight: FontWeight.w600),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    wsConnected ? '等待异常成交量信号...' : '正在连接服务器...',
                    style: TextStyle(color: Colors.grey.shade600),
                  ),
                  if (wsError != null) ...[
                    const SizedBox(height: 12),
                    Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 30),
                      child: Text(
                        wsError!,
                        textAlign: TextAlign.center,
                        style: const TextStyle(color: Colors.red),
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      );
    }

    return RefreshIndicator(
      onRefresh: () async {
        reconnectWebSocket();
      },
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
        children: [
          Row(
            children: [
              const Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      'Alerts',
                      style: TextStyle(
                        fontSize: 25,
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                    SizedBox(height: 3),
                    Text('实时异常成交量', style: TextStyle(color: Colors.grey)),
                  ],
                ),
              ),
              Text(
                '${visibleBatches.length} 批',
                style: TextStyle(color: Colors.grey.shade600),
              ),
            ],
          ),
          const SizedBox(height: 16),
          ...visibleBatches.map(buildAlertBatch),
        ],
      ),
    );
  }

  Widget buildAlertBatch(Map<String, dynamic> batch) {
    final cycle = batch['cycle_open_time'];

    final cycleOpenTime = cycle is num ? cycle.toInt() : 0;

    final dynamic rawAlerts = batch['alerts'];

    final alerts = rawAlerts is List
        ? rawAlerts
              .whereType<Map>()
              .map((item) => Map<String, dynamic>.from(item))
              .toList()
        : <Map<String, dynamic>>[];

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        buildBatchHeader(cycleOpenTime, alerts.length),
        const SizedBox(height: 6),
        buildTickerTable(alerts),
        const SizedBox(height: 22),
      ],
    );
  }

  Widget buildBatchHeader(int cycleOpenTime, int count) {
    return Row(
      children: [
        Container(
          width: 4,
          height: 22,
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(3),
            color: Theme.of(context).colorScheme.primary,
          ),
        ),
        const SizedBox(width: 9),
        Text(
          formatCycleTime(cycleOpenTime),
          style: const TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
        ),
        const SizedBox(width: 10),
        Text(
          '$count tickers',
          style: TextStyle(color: Colors.grey.shade600, fontSize: 13),
        ),
      ],
    );
  }

  Widget buildTickerTable(List<Map<String, dynamic>> alerts) {
    return Container(
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: Colors.grey.shade300),
      ),
      child: Column(
        children: [
          buildAlertTableHeader(),
          ...List.generate(alerts.length, (index) {
            return buildAlertRow(alerts[index]);
          }),
        ],
      ),
    );
  }

  Widget buildAlertTableHeader() {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
      decoration: BoxDecoration(
        color: Colors.grey.shade100,
        borderRadius: const BorderRadius.vertical(top: Radius.circular(10)),
      ),
      child: const Row(
        children: [
          Expanded(
            flex: 30,
            child: Text(
              'Ticker',
              style: TextStyle(fontSize: 12, color: Colors.grey),
            ),
          ),
          Expanded(
            flex: 25,
            child: Text(
              'RVOL 10 / 30',
              style: TextStyle(fontSize: 12, color: Colors.grey),
            ),
          ),
          Expanded(
            flex: 17,
            child: Text(
              '24h',
              textAlign: TextAlign.right,
              style: TextStyle(fontSize: 12, color: Colors.grey),
            ),
          ),
          Expanded(
            flex: 28,
            child: Text(
              'High Rate 30 / 15',
              textAlign: TextAlign.right,
              style: TextStyle(fontSize: 12, color: Colors.grey),
            ),
          ),
        ],
      ),
    );
  }

  Widget buildAlertRow(Map<String, dynamic> alert) {
    final symbol = alert['symbol']?.toString() ?? '--';

    final rvol10 = toDouble(alert['alert_rvol10']);

    final rvol30 = toDouble(alert['alert_rvol30']);

    final rate30 = toDouble(alert['high_rvol_rate30']);

    final rate15 = toDouble(alert['high_rvol_rate15']);

    final priceChange = toDouble(alert['price_change_percent']);

    final hasPriceChange = alert['has_price_change'] == true;

    final maxRVOL = rvol10 > rvol30 ? rvol10 : rvol30;

    return InkWell(
      onTap: () {
        copyTicker(symbol);
      },
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 11),
        decoration: BoxDecoration(
          border: Border(top: BorderSide(color: Colors.grey.shade200)),
        ),
        child: Row(
          children: [
            Expanded(
              flex: 30,
              child: Row(
                children: [
                  Flexible(
                    child: Text(
                      symbol,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontWeight: FontWeight.w600,
                        fontSize: 14,
                      ),
                    ),
                  ),
                  const SizedBox(width: 4),
                  Icon(
                    Icons.content_copy,
                    size: 12,
                    color: Colors.grey.shade500,
                  ),
                ],
              ),
            ),

            Expanded(
              flex: 25,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '${rvol10.toStringAsFixed(2)} / '
                    '${rvol30.toStringAsFixed(2)}',
                    style: const TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                  Text(
                    'max ${maxRVOL.toStringAsFixed(2)}',
                    style: TextStyle(fontSize: 10, color: Colors.grey.shade500),
                  ),
                ],
              ),
            ),

            Expanded(
              flex: 17,
              child: Text(
                hasPriceChange ? formatPercent(priceChange) : '--',
                textAlign: TextAlign.right,
                style: TextStyle(
                  fontSize: 13,
                  color: hasPriceChange
                      ? priceChange >= 0
                            ? Colors.green
                            : Colors.red
                      : Colors.grey,
                ),
              ),
            ),

            Expanded(
              flex: 28,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  Text(
                    '${(rate30 * 100).toStringAsFixed(1)}%',
                    style: const TextStyle(
                      fontSize: 14,
                      fontWeight: FontWeight.bold,
                    ),
                  ),
                  Text(
                    '15 ${(rate15 * 100).toStringAsFixed(1)}%',
                    style: TextStyle(fontSize: 10, color: Colors.grey.shade500),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  // ============================================================
  // Ranking 页面
  // ============================================================

  Widget buildRankingPage() {
    if (rankingLoading && ranking.isEmpty) {
      return const Center(child: CircularProgressIndicator());
    }

    if (rankingError != null && ranking.isEmpty) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text(
              'Ranking 获取失败',
              style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
            ),
            const SizedBox(height: 8),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 30),
              child: Text(rankingError!, textAlign: TextAlign.center),
            ),
            const SizedBox(height: 15),
            ElevatedButton(onPressed: loadRanking, child: const Text('重试')),
          ],
        ),
      );
    }

    return RefreshIndicator(
      onRefresh: loadRanking,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
        children: [
          Row(
            children: [
              const Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      'Ranking',
                      style: TextStyle(
                        fontSize: 25,
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                    SizedBox(height: 3),
                    Text('当前全市场 RVOL 排名', style: TextStyle(color: Colors.grey)),
                  ],
                ),
              ),
              if (rankingLoading)
                const Padding(
                  padding: EdgeInsets.only(right: 8),
                  child: SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                ),
              IconButton(
                onPressed: rankingLoading ? null : loadRanking,
                icon: const Icon(Icons.refresh),
              ),
            ],
          ),
          const SizedBox(height: 12),
          buildRankingTable(),
        ],
      ),
    );
  }

  Widget buildRankingTable() {
    return Container(
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: Colors.grey.shade300),
      ),
      child: Column(
        children: [
          buildRankingHeader(),
          ...List.generate(ranking.length, (index) {
            return buildRankingRow(ranking[index]);
          }),
        ],
      ),
    );
  }

  Widget buildRankingHeader() {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 9),
      decoration: BoxDecoration(
        color: Colors.grey.shade100,
        borderRadius: const BorderRadius.vertical(top: Radius.circular(10)),
      ),
      child: const Row(
        children: [
          SizedBox(
            width: 35,
            child: Text(
              '#',
              style: TextStyle(fontSize: 11, color: Colors.grey),
            ),
          ),
          Expanded(
            flex: 30,
            child: Text(
              'Ticker',
              style: TextStyle(fontSize: 11, color: Colors.grey),
            ),
          ),
          Expanded(
            flex: 28,
            child: Text(
              'RVOL 10 / 30',
              style: TextStyle(fontSize: 11, color: Colors.grey),
            ),
          ),
          Expanded(
            flex: 15,
            child: Text(
              '24h',
              textAlign: TextAlign.right,
              style: TextStyle(fontSize: 11, color: Colors.grey),
            ),
          ),
          Expanded(
            flex: 27,
            child: Text(
              'High Rate 30 / 15',
              textAlign: TextAlign.right,
              style: TextStyle(fontSize: 11, color: Colors.grey),
            ),
          ),
        ],
      ),
    );
  }

  Widget buildRankingRow(Map<String, dynamic> item) {
    final rank = toInt(item['rank']);

    final symbol = item['symbol']?.toString() ?? '--';

    final rvol10 = toDouble(item['rvol10']);

    final rvol30 = toDouble(item['rvol30']);

    final rankRVOL = toDouble(item['rank_rvol']);

    final rate30 = toDouble(item['high_rvol_rate30']);
    final rate15 = toDouble(item['high_rvol_rate15']);

    final priceChange = toDouble(item['price_change_percent']);
    final hasPriceChange = item['has_price_change'] == true;

    return InkWell(
      onTap: () {
        copyTicker(symbol);
      },
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 11),
        decoration: BoxDecoration(
          border: Border(top: BorderSide(color: Colors.grey.shade200)),
        ),
        child: Row(
          children: [
            SizedBox(
              width: 35,
              child: Text(
                '$rank',
                style: TextStyle(fontSize: 12, color: Colors.grey.shade600),
              ),
            ),

            Expanded(
              flex: 30,
              child: Row(
                children: [
                  Flexible(
                    child: Text(
                      symbol,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                  const SizedBox(width: 3),
                  Icon(
                    Icons.content_copy,
                    size: 11,
                    color: Colors.grey.shade500,
                  ),
                ],
              ),
            ),

            Expanded(
              flex: 28,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '${rvol10.toStringAsFixed(2)} / '
                    '${rvol30.toStringAsFixed(2)}',
                    style: const TextStyle(
                      fontSize: 12,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                  Text(
                    'rank ${rankRVOL.toStringAsFixed(2)}',
                    style: TextStyle(
                      fontSize: 10,
                      color: Colors.grey.shade500,
                    ),
                  ),
                ],
              ),
            ),

            Expanded(
              flex: 15,
              child: Text(
                hasPriceChange ? formatPercent(priceChange) : '--',
                textAlign: TextAlign.right,
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w500,
                  color: hasPriceChange
                      ? priceChange >= 0
                          ? Colors.green
                          : Colors.red
                      : Colors.grey,
                ),
              ),
            ),

            Expanded(
              flex: 27,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  Text(
                    '${(rate30 * 100).toStringAsFixed(1)}%',
                    style: const TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.bold,
                    ),
                  ),
                  Text(
                    '15 ${(rate15 * 100).toStringAsFixed(1)}%',
                    style: TextStyle(
                      fontSize: 10,
                      color: Colors.grey.shade500,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  // ============================================================
  // Bottom Navigation
  // ============================================================

  Widget buildNavigationBar() {
    return NavigationBar(
      selectedIndex: selectedTab,
      onDestinationSelected: (index) {
        setState(() {
          selectedTab = index;
        });

        if (index == 1) {
          loadRanking();
        }
      },
      destinations: const [
        NavigationDestination(
          icon: Icon(Icons.notifications_outlined),
          selectedIcon: Icon(Icons.notifications),
          label: 'Alerts',
        ),
        NavigationDestination(
          icon: Icon(Icons.leaderboard_outlined),
          selectedIcon: Icon(Icons.leaderboard),
          label: 'Ranking',
        ),
      ],
    );
  }

  // ============================================================
  // Clipboard
  // ============================================================

  Future<void> copyTicker(String symbol) async {
    await Clipboard.setData(ClipboardData(text: symbol));

    if (!mounted) {
      return;
    }

    ScaffoldMessenger.of(context).hideCurrentSnackBar();

    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text('已复制 $symbol'),
        duration: const Duration(milliseconds: 900),
      ),
    );
  }

  // ============================================================
  // 辅助函数
  // ============================================================

  double toDouble(dynamic value) {
    if (value is num) {
      return value.toDouble();
    }

    if (value is String) {
      return double.tryParse(value) ?? 0;
    }

    return 0;
  }

  int toInt(dynamic value) {
    if (value is num) {
      return value.toInt();
    }

    if (value is String) {
      return int.tryParse(value) ?? 0;
    }

    return 0;
  }

  String formatPercent(double value) {
    final sign = value >= 0 ? '+' : '';

    return '$sign${value.toStringAsFixed(2)}%';
  }

  String formatCycleTime(int openTime) {
    if (openTime <= 0) {
      return '--:--';
    }

    final date = DateTime.fromMillisecondsSinceEpoch(openTime).toLocal();

    final hour = date.hour.toString().padLeft(2, '0');

    final minute = date.minute.toString().padLeft(2, '0');

    return '$hour:$minute';
  }
}
