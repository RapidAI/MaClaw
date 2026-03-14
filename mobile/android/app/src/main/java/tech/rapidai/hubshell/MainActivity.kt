package tech.rapidai.hubshell

import android.annotation.SuppressLint
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.webkit.CookieManager
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AppCompatActivity

class MainActivity : AppCompatActivity() {
    private lateinit var webView: WebView

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        webView = WebView(this)
        setContentView(webView)

        if (BuildConfig.DEBUG) {
            WebView.setWebContentsDebuggingEnabled(true)
        }

        val settings = webView.settings
        settings.javaScriptEnabled = true
        settings.domStorageEnabled = true
        settings.databaseEnabled = true
        settings.allowFileAccess = true
        settings.allowContentAccess = true
        settings.loadsImagesAutomatically = true
        settings.cacheMode = WebSettings.LOAD_DEFAULT
        settings.mediaPlaybackRequiresUserGesture = false
        settings.mixedContentMode = WebSettings.MIXED_CONTENT_ALWAYS_ALLOW

        CookieManager.getInstance().setAcceptCookie(true)
        CookieManager.getInstance().setAcceptThirdPartyCookies(webView, true)

        webView.webChromeClient = WebChromeClient()
        webView.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView?, request: WebResourceRequest?): Boolean {
                val target = request?.url?.toString()?.trim().orEmpty()
                if (target.isEmpty()) {
                    return false
                }
                if (
                    target.startsWith("http://") ||
                    target.startsWith("https://") ||
                    target.startsWith("file:///android_asset/")
                ) {
                    return false
                }
                startActivity(Intent(Intent.ACTION_VIEW, request?.url))
                return true
            }
        }

        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (webView.canGoBack()) {
                    webView.goBack()
                } else {
                    finish()
                }
            }
        })

        webView.loadUrl(resolveLaunchUrl(intent?.dataString))
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        val deepLink = intent.dataString?.trim().orEmpty()
        if (deepLink.isNotEmpty()) {
            webView.loadUrl(deepLink)
        }
    }

    private fun resolveLaunchUrl(deepLink: String?): String {
        if (!deepLink.isNullOrBlank()) {
            return deepLink
        }
        val configuredStart = BuildConfig.DEFAULT_START_URL.trim()
        if (configuredStart.isNotEmpty()) {
            return configuredStart
        }
        val hubCenter = BuildConfig.DEFAULT_HUBCENTER_URL.trim()
        if (hubCenter.isEmpty()) {
            return "file:///android_asset/bootstrap.html"
        }
        return "file:///android_asset/bootstrap.html?hubcenter=" + Uri.encode(hubCenter)
    }
}
