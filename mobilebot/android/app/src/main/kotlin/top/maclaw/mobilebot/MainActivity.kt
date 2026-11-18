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
                    // null = could not determine (unreadable archive, no
                    // signer certs) — the Dart side then lets the installer
                    // proceed and report its own error.
                    result.success(path?.let { apkSignatureMatches(it) })
                }
                else -> result.notImplemented()
            }
        }
    }

    /**
     * Compares the downloaded APK's signing certificate against the installed
     * app's own. `false` is a REAL mismatch (the OS would fail the install
     * with INSTALL_FAILED_UPDATE_INCOMPATIBLE); `null` means undeterminable:
     * the archive could not be parsed, holds a different applicationId, or
     * its signer set is empty. The empty case matters — `getPackageArchiveInfo`
     * returns no `signingInfo` on Android 9/10, and a v2-only APK exposes no
     * legacy `signatures` below API 28, so treating the empty set as a
     * mismatch would block every legitimate same-key update on those devices
     * with the "签名不一致" error.
     */
    private fun apkSignatureMatches(apkPath: String): Boolean? {
        return try {
            val pm = packageManager
            val flag = signingFlags()
            val mine = pm.getPackageInfo(packageName, flag)
            val apkInfo = pm.getPackageArchiveInfo(apkPath, flag) ?: return null
            // A different applicationId installs as a separate app — no
            // update conflict either way; leave the decision to the installer.
            if (apkInfo.packageName != packageName) return null
            val apkCerts = certHashes(apkInfo)
            val myCerts = certHashes(mine)
            if (apkCerts.isEmpty() || myCerts.isEmpty()) return null
            apkCerts == myCerts
        } catch (e: Exception) {
            null
        }
    }

    /**
     * GET_SIGNING_CERTIFICATES fills `signingInfo` (installed apps, and
     * archives on API 30+); GET_SIGNATURES keeps the legacy `signatures`
     * list readable, which is the only source that works on Android 9/10
     * archives and pre-P devices — request both and prefer signingInfo.
     */
    private fun signingFlags(): Int =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            PackageManager.GET_SIGNING_CERTIFICATES or
                @Suppress("DEPRECATION") PackageManager.GET_SIGNATURES
        } else {
            @Suppress("DEPRECATION")
            PackageManager.GET_SIGNATURES
        }

    private fun certHashes(info: android.content.pm.PackageInfo): Set<String> {
        val signers: Array<Signature> =
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                val viaSigningInfo = info.signingInfo?.apkContentsSigners
                if (!viaSigningInfo.isNullOrEmpty()) {
                    viaSigningInfo
                } else {
                    @Suppress("DEPRECATION")
                    info.signatures ?: emptyArray()
                }
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
