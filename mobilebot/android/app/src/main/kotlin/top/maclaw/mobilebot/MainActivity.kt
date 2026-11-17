package top.maclaw.mobilebot

import android.content.pm.PackageManager
import android.content.pm.Signature
import android.os.Build
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.security.MessageDigest

class MainActivity : FlutterActivity() {
    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger,
            "maclawgo/update",
        ).setMethodCallHandler { call, result ->
            when (call.method) {
                "apkSignatureMatches" -> {
                    val path = call.argument<String>("path")
                    result.success(
                        path != null && apkSignatureMatches(path),
                    )
                }
                else -> result.notImplemented()
            }
        }
    }

    /**
     * Compares the downloaded APK's signing certificate against the installed
     * app's own. A mismatch means the OS would fail the install with
     * INSTALL_FAILED_UPDATE_INCOMPATIBLE — the updater surfaces a friendly
     * "uninstall first" hint instead.
     */
    private fun apkSignatureMatches(apkPath: String): Boolean {
        val pm = packageManager
        val flag = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            PackageManager.GET_SIGNING_CERTIFICATES
        } else {
            @Suppress("DEPRECATION")
            PackageManager.GET_SIGNATURES
        }
        val apkInfo = pm.getPackageArchiveInfo(apkPath, flag) ?: return false
        val mine = pm.getPackageInfo(packageName, flag)
        return certHashes(apkInfo) == certHashes(mine)
    }

    private fun certHashes(info: android.content.pm.PackageInfo): Set<String> {
        val signers: Array<Signature> =
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                info.signingInfo?.apkContentsSigners ?: emptyArray()
            } else {
                @Suppress("DEPRECATION")
                info.signatures ?: emptyArray()
            }
        val md = MessageDigest.getInstance("SHA-256")
        return signers
            .map { md.digest(it.toByteArray()).joinToString("") { b -> "%02x".format(b) } }
            .toSet()
    }
}
