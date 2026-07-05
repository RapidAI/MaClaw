import Flutter
import UIKit
import XCTest

class RunnerTests: XCTestCase {

  func testMaClawMobileBundleConfiguration() {
    let bundle = Bundle(for: RunnerTests.self)

    XCTAssertEqual(bundle.bundleIdentifier, "top.mypapers.maclaw.mobile.RunnerTests")
  }

}
