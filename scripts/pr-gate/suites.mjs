// Named Go suites for the pull-request gate.
// The -run expression is built from these names so a rename fails the
// selector tests instead of matching zero tests and exiting 0.

export const lifecycleSuite = {
  id: "lifecycle",
  race: true,
  timeout: "300s",
  packages: {
    "./MaClawSrv": [
      "TestDesktopReceiptPersistsVisibleOutcome",
      "TestDesktopDeliverableExtractionChain",
      "TestPlanFileIsNotDelivery",
      "TestTextItemsNeedExplicitIDs",
      "TestHTTPReadDuringFinalize",
      "TestDesktopReceiptStaysUnreadyPastTwoSeconds",
      "TestDesktopAbandonedReceiptBecomesUnverified",
      "TestDesktopReceiptPersistFailureIsNotReady",
      "TestBotPDFWorksWithAndWithoutDesktopAndAttachesToTurn",
      "TestNoDesktopAsyncFinishKeepsGeneratedPDF",
    ],
    "./corelib/agentservice": [
      "TestDesktopDeliverablesOnlyForExecuteDesktopTurns",
    ],
  },
};

export const hubOutcomeSuite = {
  id: "hub-outcome",
  race: false,
  timeout: "240s",
  packages: {
    "./hub/internal/botmgmt": [
      "TestDesktopRunTaskOutcomeReachesEveryReader",
      "TestDelegatedRunEndHonoursTaskOutcome",
      "TestSettleFinishedRunAgreesWithUndeliveredReceipt",
      "TestDelegatedRunEndLeavesRunningJobOpen",
    ],
  },
};

export function anchoredRun(names) {
  for (const name of names) {
    if (!/^Test[A-Za-z0-9_]+$/.test(name)) {
      throw new Error(`refusing unsafe test name ${name}`);
    }
  }
  return `^(${names.join("|")})$`;
}

export function suiteRun(suite) {
  const names = [];
  for (const list of Object.values(suite.packages)) names.push(...list);
  return anchoredRun(names);
}
