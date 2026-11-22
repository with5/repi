package io.github.with5.repi.ui

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext

// The green of the desktop app's icon.
private val Green = Color(0xFF4A9A68)
private val LightGreen = Color(0xFF9CF0BD)

private val Dark = darkColorScheme(
    primary = LightGreen,
    onPrimary = Color(0xFF0B120E),
    secondary = Green,
    background = Color(0xFF0F1A14),
    surface = Color(0xFF0F1A14),
    surfaceContainer = Color(0xFF17261D),
)
private val Light = lightColorScheme(primary = Color(0xFF2F7A52), secondary = Green)

/** Material You colors on Android 12 and later, the app's green before. */
@Composable
fun RepiTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val colors = when {
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S -> {
            val ctx = LocalContext.current
            if (dark) dynamicDarkColorScheme(ctx) else dynamicLightColorScheme(ctx)
        }
        dark -> Dark
        else -> Light
    }
    MaterialTheme(colorScheme = colors, content = content)
}
