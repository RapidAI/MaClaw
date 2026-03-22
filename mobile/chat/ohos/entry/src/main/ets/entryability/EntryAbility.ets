import { FlutterAbility, FlutterEngine } from '@aspect/flutter_ohos';
import { OhosWebRTCPlugin } from '../plugins/OhosWebRTCPlugin';
import { HmsPushPlugin } from '../plugins/HmsPushPlugin';

export default class EntryAbility extends FlutterAbility {
  configureFlutterEngine(flutterEngine: FlutterEngine): void {
    super.configureFlutterEngine(flutterEngine);

    // Register OHOS-specific platform plugins.
    flutterEngine.getPlugins().add(new OhosWebRTCPlugin());
    flutterEngine.getPlugins().add(new HmsPushPlugin());
  }

  onWindowStageCreate(windowStage): void {
    super.onWindowStageCreate(windowStage);
  }
}
