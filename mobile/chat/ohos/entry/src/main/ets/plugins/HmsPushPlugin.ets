import { FlutterPlugin, MethodCall, MethodChannel, MethodResult } from '@aspect/flutter_ohos';
// import push from '@hms.core.push';

/**
 * Native OHOS plugin for HMS Push Kit integration.
 *
 * Methods:
 *  - getToken: Get the HMS push token
 *
 * Events (native → Flutter):
 *  - onTokenRefresh: Token was refreshed
 *  - onMessageReceived: Data message received
 *  - onNotificationTap: User tapped a notification
 */
export class HmsPushPlugin implements FlutterPlugin {
  private channel: MethodChannel | null = null;

  onAttachedToEngine(binding: FlutterPlugin.FlutterPluginBinding): void {
    this.channel = new MethodChannel(
      binding.getBinaryMessenger(),
      'com.maclaw.chat/hms_push'
    );
    this.channel.setMethodCallHandler(this.onMethodCall.bind(this));

    // TODO: Register HMS push message receiver
    // push.on('receiveMessage', (data) => {
    //   this.channel?.invokeMethod('onMessageReceived', data);
    // });
    // push.on('receiveNotificationClick', (data) => {
    //   this.channel?.invokeMethod('onNotificationTap', data);
    // });
  }

  onDetachedFromEngine(): void {
    this.channel?.setMethodCallHandler(null);
    this.channel = null;
  }

  async onMethodCall(call: MethodCall, result: MethodResult): Promise<void> {
    switch (call.method) {
      case 'getToken':
        try {
          // TODO: const token = await push.getToken();
          const token = 'hms_token_placeholder';
          result.success(token);
        } catch (e) {
          result.error('HMS_ERROR', 'Failed to get HMS push token', null);
        }
        break;

      default:
        result.notImplemented();
    }
  }
}
