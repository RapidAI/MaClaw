import java.util.Properties

plugins {
    id("com.android.application")
    id("kotlin-android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

val maclawKeystorePropertiesFile = rootProject.file("key.properties")
val maclawKeystoreProperties = Properties()
val maclawReleaseSigningConfigured = maclawKeystorePropertiesFile.exists()
if (maclawReleaseSigningConfigured) {
    maclawKeystorePropertiesFile.inputStream().use { maclawKeystoreProperties.load(it) }
}

gradle.taskGraph.whenReady {
    val releaseTaskRequested = allTasks.any { task ->
        task.path.endsWith(":app:assembleRelease") || task.path.endsWith(":app:bundleRelease")
    }
    if (releaseTaskRequested && !maclawReleaseSigningConfigured) {
        throw GradleException(
            "MaClaw Mobile release signing requires android/key.properties with storeFile, storePassword, keyAlias, and keyPassword."
        )
    }
}


android {
    namespace = "top.mypapers.maclaw.mobile"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
        isCoreLibraryDesugaringEnabled = true
    }

    kotlinOptions {
        jvmTarget = JavaVersion.VERSION_17.toString()
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "top.mypapers.maclaw.mobile"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }













    signingConfigs {
        if (maclawReleaseSigningConfigured) {
            create("release") {
                keyAlias = maclawKeystoreProperties["keyAlias"] as String
                keyPassword = maclawKeystoreProperties["keyPassword"] as String
                storeFile = file(maclawKeystoreProperties["storeFile"] as String)
                storePassword = maclawKeystoreProperties["storePassword"] as String
            }
        }
    }

    buildTypes {
        release {
            if (maclawReleaseSigningConfigured) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }
}

flutter {
    source = "../.."
}
dependencies {
    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.1.5")
}