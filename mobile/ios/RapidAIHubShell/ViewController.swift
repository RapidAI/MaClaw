import UIKit
import WebKit

final class ViewController: UIViewController, UITextFieldDelegate {
    private let textField = UITextField(frame: .zero)
    private let button = UIButton(type: .system)
    private let refreshButton = UIButton(type: .system)
    private let stackView = UIStackView(frame: .zero)
    private let webView: WKWebView = {
        let config = WKWebViewConfiguration()
        return WKWebView(frame: .zero, configuration: config)
    }()

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = UIColor(red: 244.0 / 255.0, green: 247.0 / 255.0, blue: 251.0 / 255.0, alpha: 1)
        configureControls()
        configureWebView()
        loadInitialPage(prefillEmail: nil)
    }

    private func configureControls() {
        [textField, button, refreshButton, stackView, webView].forEach { $0.translatesAutoresizingMaskIntoConstraints = false }

        textField.borderStyle = .roundedRect
        textField.placeholder = "name@example.com"
        textField.keyboardType = .emailAddress
        textField.autocapitalizationType = .none
        textField.autocorrectionType = .no
        textField.returnKeyType = .go
        textField.delegate = self

        button.configuration = .filled()
        button.configuration?.title = "Open PWA"
        button.addTarget(self, action: #selector(openUsingEmail), for: .touchUpInside)

        refreshButton.configuration = .tinted()
        refreshButton.configuration?.title = "↻"
        refreshButton.addTarget(self, action: #selector(refreshPage), for: .touchUpInside)

        stackView.axis = .horizontal
        stackView.spacing = 12
        stackView.addArrangedSubview(textField)
        stackView.addArrangedSubview(button)
        stackView.addArrangedSubview(refreshButton)
        button.widthAnchor.constraint(equalToConstant: 100).isActive = true
        refreshButton.widthAnchor.constraint(equalToConstant: 44).isActive = true

        view.addSubview(stackView)
        view.addSubview(webView)

        NSLayoutConstraint.activate([
            stackView.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 12),
            stackView.leadingAnchor.constraint(equalTo: view.leadingAnchor, constant: 16),
            stackView.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -16),
            webView.topAnchor.constraint(equalTo: stackView.bottomAnchor, constant: 12),
            webView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            webView.bottomAnchor.constraint(equalTo: view.bottomAnchor),
        ])
    }

    private func configureWebView() {
        webView.allowsBackForwardNavigationGestures = true
        if #available(iOS 16.4, *) {
            webView.isInspectable = true
        }
    }

    private func loadInitialPage(prefillEmail: String?) {
        let start = AppConfiguration.startURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if !start.isEmpty && start.lowercased() != "bootstrap", let url = URL(string: start) {
            webView.load(URLRequest(url: url))
            return
        }

        guard let bootstrapURL = Bundle.main.url(forResource: "bootstrap", withExtension: "html", subdirectory: "Resources"),
              let htmlData = try? Data(contentsOf: bootstrapURL),
              var htmlString = String(data: htmlData, encoding: .utf8)
        else {
            return
        }

        // Inject hubcenter and prefill_email as JS variables so the page works
        // without relying on query parameters (which don't exist under loadHTMLString).
        var injection = "window.__injectedHubCenter = \(jsStringLiteral(AppConfiguration.hubCenterURL));"
        if let prefillEmail, !prefillEmail.isEmpty {
            injection += "\nwindow.__injectedPrefillEmail = \(jsStringLiteral(prefillEmail));"
        }
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

    @objc private func openUsingEmail() {
        let email = textField.text?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !email.isEmpty else {
            textField.becomeFirstResponder()
            return
        }

        let start = AppConfiguration.startURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if !start.isEmpty && start.lowercased() != "bootstrap", var components = URLComponents(string: start) {
            var queryItems = components.queryItems ?? []
            if !queryItems.contains(where: { $0.name == "email" }) {
                queryItems.append(URLQueryItem(name: "email", value: email))
            }
            if !queryItems.contains(where: { $0.name == "entry" }) {
                queryItems.append(URLQueryItem(name: "entry", value: "app"))
            }
            components.queryItems = queryItems
            if let url = components.url {
                webView.load(URLRequest(url: url))
                return
            }
        }

        loadInitialPage(prefillEmail: email)
    }

    func textFieldShouldReturn(_ textField: UITextField) -> Bool {
        openUsingEmail()
        return true
    }

    @objc private func refreshPage() {
        // Clear website caches from the webView's own data store, then reload
        let dataStore = webView.configuration.websiteDataStore
        let dataTypes: Set<String> = [
            WKWebsiteDataTypeDiskCache,
            WKWebsiteDataTypeMemoryCache,
            WKWebsiteDataTypeOfflineWebApplicationCache,
            WKWebsiteDataTypeFetchCache
        ]
        dataStore.removeData(ofTypes: dataTypes, modifiedSince: .distantPast) { [weak self] in
            DispatchQueue.main.async {
                self?.webView.reloadFromOrigin()
            }
        }
    }
}
