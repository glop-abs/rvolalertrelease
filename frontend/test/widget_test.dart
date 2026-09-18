import 'package:flutter_test/flutter_test.dart';

import 'package:rvol_alert_app/main.dart';

void main() {
  testWidgets('RVOL Alert app renders', (WidgetTester tester) async {
    await tester.pumpWidget(const RvolAlertApp(enableNetwork: false));

    expect(find.text('RVOL Alert'), findsOneWidget);
    expect(find.text('Alerts'), findsOneWidget);
    expect(find.text('Ranking'), findsOneWidget);
  });
}
