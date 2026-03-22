package tech.rapidai.hubshell

import android.annotation.SuppressLint
import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.view.Gravity
import android.widget.Button
import android.widget.FrameLayout
import android.webkit.CookieManager
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.OnBackPressedCallback
import androidx.activity.result.ActivityResultLauncher
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity

class MainActivity : AppCompatActivity() {
    private lateinit var webView: WebView
    private var fileUploadCallback: ValueCallback<Array<Uri>>? = null
    private lateinit var fileChooserLauncher: ActivityResultLauncher<Intent>

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        webView = WebView(this)

        // Create a FrameLayout with WebView + floating refresh button
        val container = FrameLayout(this)
        container.addView(webView, FrameLayout.LayoutParams(
            FrameLayout.LayoutParams.MATCH_PARENT,
            FrameLayout.LayoutParams.MATCH_PARENT
        ))

        val refreshBtn = Button(this).apply {
            text = "↻"
            textSize = 18f
            val size = (48 * resources.displayMetrics.density).toInt()
            val margin = (16 * resources.displayMetrics.density).toInt()
            val lp = FrameLayout.LayoutParams(size, size).apply {
                gravity = Gravity.BOTTOM or Gravity.END
                setMargins(margin, margin, margin, margin)
            }
            layoutParams = lp
            alpha = 0.85f
            setPadding(0, 0, 0, 0)
            visibility = android.view.View.GONE
            setOnClickListener {
                webView.clearCache(true)
                webView.reload()
            }
        }
        container.addView(refreshBtn)

        setContentView(container)

        // Register file chooser result handler
        fileChooserLauncher = registerForActivityResult(
            ActivityResultContracts.StartActivityForResult()
        ) { result ->
            val uris = if (result.resultCode == Activity.RESULT_OK) {
                val data = result.data
                if (data?.clipData != null) {
                    Array(data.clipData!!.itemCount) { i -> data.clipData!!.getItemAt(i).uri }
                } else {
                    data?.data?.let { arrayOf(it) }
                }
            } else null
            fileUploadCallback?.onReceiveValue(uris)
            fileUploadCallback = null
        }

        if (BuildConfig.DEBUG) {
            WebView.setWebContentsDebuggingEnabled(true)
        }

        val settings = webView.settings
        settings.javaScriptEnabled = true
        settings.domStorageEnabled = true
        settings.allowFileAccess = true
        settings.allowContentAccess = true
        @Suppress("DEPRECATION")
        settings.allowUniversalAccessFromFileURLs = true
        settings.loadsImagesAutomatically = true
        settings.cacheMode = WebSettings.LOAD_NO_CACHE
        settings.mediaPlaybackRequiresUserGesture = false
        settings.mixedContentMode = WebSettings.MIXED_CONTENT_ALWAYS_ALLOW

        CookieManager.getInstance().setAcceptCookie(true)
        CookieManager.getInstance().setAcceptThirdPartyCookies(webView, true)

        webView.webChromeClient = object : WebChromeClient() {
            override fun onShowFileChooser(
                webView: WebView?,
                filePathCallback: ValueCallback<Array<Uri>>?,
                fileChooserParams: FileChooserParams?
            ): Boolean {
                // Cancel any pending callback
                fileUploadCallback?.onReceiveValue(null)
                fileUploadCallback = filePathCallback

                val intent = fileChooserParams?.createIntent() ?: Intent(Intent.ACTION_GET_CONTENT).apply {
                    type = "image/*"
                    addCategory(Intent.CATEGORY_OPENABLE)
                }
                fileChooserLauncher.launch(intent)
                return true
            }
        }
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
