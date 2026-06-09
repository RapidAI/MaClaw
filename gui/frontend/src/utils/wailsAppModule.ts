import * as WailsApp from "../../wailsjs/go/main/App";

export type WailsAppModule = typeof import("../../wailsjs/go/main/App");

const wailsAppModulePromise = Promise.resolve(WailsApp as WailsAppModule);

export function getWailsAppModule(): Promise<WailsAppModule> {
    return wailsAppModulePromise;
}
