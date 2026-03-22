import UIKit
import WebKit

final class ViewController: UIViewController {
    private let webView: WKWebView = {
        let config = WKWebViewConfiguration()
        return WKWebView(frame: .zero, configuration: config)
    }()

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = UIColor(red: 244.0 / 255.0, green: 247.0 / 255.0, blue: 251.0 / 255.0, alpha: 1)
        configureWebView()
        loadInitialPage()
    }

    private func configureWebView() {
        webView.translatesAutoresizingMaskIntoConstraints = false
        webView.allowsBackForwardNavigationGestures = true
        #if DEBUG
        if #available(iOS 16.4, *) {
            webView.isInspectable = true
        }
        #endif

        view.addSubview(webView)
        NSLayoutConstraint.activate([
            webView.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
            webView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            webView.bottomAnchor.constraint(equalTo: view.bottomAnchor),
        ])
    }

    private func loadInitialPage() {
        let start = AppConfiguration.startURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if !start.isEmpty && start.lowercased() != "bootstrap", let url = URL(string: start) {
            webView.load(URLRequest(url: url))
            return
        }

        guard let bootstrapURL = Bundle.main.url(forResource: "bootstrap", withExtension: "html")
                ?? Bundle.main.url(forResource: "bootstrap", withExtension: "html", subdirectory: "Resources"),
              let htmlData = try? Data(contentsOf: bootstrapURL),
              var htmlString = String(data: htmlData, encoding: .utf8)
        else {
            return
        }

        // Inject hubcenter as a JS variable so the page works without query parameters.
        let injection = "window.__injectedHubCenter = \(jsStringLiteral(AppConfiguration.hubCenterURL));"
        htmlString = htmlString.replacingOccurrences(
            of: "<script>",
            with: "<script>\n\(injection)\n",
            options: [],
            range: htmlString.range(of: "<script>")
        )

        // Use the hubCenterURL as baseURL so fetch() calls are not blocked by
        // cross-origin restrictions (file:// → http:// is forbidden in WKWebView).
        let baseURL = URL(string: AppConfiguration.hubCenterURL) ?? URL(string: "about:blank")!
        webView.loadHTMLString(htmlString, baseURL: baseURL)
    }

    private func jsStringLiteral(_ s: String) -> String {
        let escaped = s
            .replacingOccurrences(of: "\\", with: "\\\\")
            .replacingOccurrences(of: "\"", with: "\\\"")
            .replacingOccurrences(of: "\n", with: "\\n")
        return "\"\(escaped)\""
    }
}
