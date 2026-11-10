// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.diag

import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.ApiException
import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectCode
import io.github.anatolijlaptev1991_ctrl.lavray.api.errorFromBody
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingDescriptor
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingValue
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.BackupErrors
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.BackupRules
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.PasswordIssue
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AppEntry
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AppFilter
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.NumberInput
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.SettingKeys
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.SettingsMapper
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.SettingsState
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppMode
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppSelection
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.TunOptions
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Base64

class SettingsTests {
    @Test fun stackIsNotOffered() {
        // System and mixed pass no data on a phone; the core forces gVisor (core-overlay/patch.go).
        assertFalse(SettingKeys.TUN_STACK in SettingKeys.SHOWN)
        val listed = listOf(SettingDescriptor(key = SettingKeys.TUN_STACK), SettingDescriptor(key = SettingKeys.TUN_MTU))
        assertFalse(SettingKeys.TUN_STACK in SettingKeys.available(listed))
    }

    @Test fun mtuIsCheckedAgainstTheCoreRange() {
        assertEquals(NumberInput.Valid(1500), SettingsMapper.parseMtu(" 1500 "))
        assertEquals(NumberInput.Valid(9000), SettingsMapper.parseMtu("9000"))
        assertEquals(NumberInput.OutOfRange, SettingsMapper.parseMtu("1279"))
        assertEquals(NumberInput.OutOfRange, SettingsMapper.parseMtu("9001"))
        assertEquals(NumberInput.NotANumber, SettingsMapper.parseMtu("15oo"))
        assertEquals(NumberInput.NotANumber, SettingsMapper.parseMtu(""))
    }

    @Test fun concurrencyAllowsMinusOneOrOneToAThousand() {
        assertEquals(NumberInput.Valid(-1), SettingsMapper.parseConcurrency("-1"))
        assertEquals(NumberInput.Valid(8), SettingsMapper.parseConcurrency("8"))
        assertEquals(NumberInput.OutOfRange, SettingsMapper.parseConcurrency("0"))
        assertEquals(NumberInput.OutOfRange, SettingsMapper.parseConcurrency("1025"))
    }

    @Test fun excludedAddressesMustBeAddressesOrNetworks() {
        assertEquals(listOf("10.0.0.0/8", "fd00::/8"), SettingsMapper.lines("# home\n10.0.0.0/8\n\n  fd00::/8  \n"))
        assertNull(SettingsMapper.firstInvalidAddress("192.168.0.0/16\n8.8.8.8\n2001:db8::1/64"))
        assertEquals("example.com", SettingsMapper.firstInvalidAddress("10.0.0.1\nexample.com"))
        assertEquals("300.1.1.1", SettingsMapper.firstInvalidAddress("300.1.1.1"))
        assertEquals("10.0.0.0/33", SettingsMapper.firstInvalidAddress("10.0.0.0/33"))
        assertEquals("::1/129", SettingsMapper.firstInvalidAddress("::1/129"))
    }

    @Test fun keysTheCoreDoesNotListAreHidden() {
        fun d(key: String) = SettingDescriptor(key = key)
        assertEquals(SettingKeys.SHOWN.toSet(), SettingKeys.available(emptyList()))
        val some = SettingKeys.available(listOf(d(SettingKeys.TUN_MTU), d("inbound.port"), d("update.channel")))
        assertEquals(setOf(SettingKeys.TUN_MTU), some)
        assertFalse("inbound.port" in SettingKeys.SHOWN)
        assertFalse(SettingKeys.SHOWN.any { it.startsWith("hotkeys.") || it.startsWith("update.") || it.startsWith("inbound.") })
    }

    @Test fun descriptorDefaultsAreReadFromJson() {
        assertEquals(9000L, SettingsMapper.defaultInt(SettingDescriptor("tun.mtu", defaultValueJson = "9000")))
        assertEquals("h2mux", SettingsMapper.defaultString(SettingDescriptor("mux.protocol", defaultValueJson = "\"h2mux\"")))
        assertEquals(true, SettingsMapper.defaultBool(SettingDescriptor("connect.health_check", defaultValueJson = "true")))
        assertNull(SettingsMapper.defaultInt(SettingDescriptor("x", defaultValueJson = "\"a\"")))
        assertNull(SettingsMapper.defaultBool(null))
    }

    @Test fun stateReadsStoredThenDefaultThenFallback() {
        val s = SettingsState(
            values = mapOf(SettingKeys.TUN_MTU to SettingValue(intValue = 1400)),
            descriptors = mapOf(
                SettingKeys.TUN_MTU to SettingDescriptor(SettingKeys.TUN_MTU, defaultValueJson = "9000"),
                SettingKeys.HEALTH_CHECK to SettingDescriptor(SettingKeys.HEALTH_CHECK, defaultValueJson = "true"),
            ),
        )
        assertEquals(1400L, s.int(SettingKeys.TUN_MTU))
        assertTrue(s.bool(SettingKeys.HEALTH_CHECK))
        assertEquals(7L, s.int("unknown", 7))
    }

    // ---- per-app selection

    private val core = TunOptions(mtu = 1500, inet4 = listOf("172.19.0.1/30"), excludePackages = listOf("com.core.excluded"))

    @Test fun allAppsLeavesTheCoreOptionsAlone() {
        assertEquals(core, AppSelection().applyTo(core))
        assertEquals(core, AppSelection(AppMode.All, setOf("a.b")).applyTo(core))
    }

    @Test fun onlySelectedBecomesTheAllowList() {
        val out = AppSelection(AppMode.Only, setOf("org.z.app", "org.a.app")).applyTo(core)
        assertEquals(listOf("org.a.app", "org.z.app"), out.includePackages)
        assertTrue(out.excludePackages.isEmpty())
    }

    @Test fun allExceptSelectedAddsToTheCoreExclusions() {
        val out = AppSelection(AppMode.Except, setOf("org.a.app", "com.core.excluded")).applyTo(core)
        assertTrue(out.includePackages.isEmpty())
        assertEquals(listOf("com.core.excluded", "org.a.app"), out.excludePackages)
    }

    @Test fun anEmptyAllowListIsAllAppsAndSaysSo() {
        val sel = AppSelection(AppMode.Only)
        assertEquals(AppMode.All, sel.effectiveMode)
        assertEquals(core, sel.applyTo(core))
        assertEquals(AppMode.Except, AppSelection(AppMode.Except).effectiveMode)
    }

    @Test fun selectionSurvivesAJsonRoundTripAndBadInputIsHarmless() {
        val sel = AppSelection(AppMode.Except, setOf("org.a.app", "org.b.app"))
        assertEquals(sel, AppSelection.parse(sel.toJson()))
        assertEquals(AppSelection(), AppSelection.parse(null))
        assertEquals(AppSelection(), AppSelection.parse("not json"))
        assertEquals(AppSelection(AppMode.All), AppSelection.parse("""{"mode":"weird"}"""))
        val dirty = AppSelection.parse("""{"mode":"only","packages":["good.pkg","../etc","nodot","a b.c"]}""")
        assertEquals(setOf("good.pkg"), dirty.packages)
        assertEquals(sel.toggled("org.a.app").packages, setOf("org.b.app"))
        assertEquals(sel.toggled("org.c.app").packages.size, 3)
    }

    @Test fun pickerListsChosenFirstHidesSystemAppsUnlessAsked() {
        val apps = listOf(
            AppEntry("com.b", "Beta", false), AppEntry("com.a", "alpha", false),
            AppEntry("com.sys", "Settings", true), AppEntry("com.z", "Zeta", false),
        )
        assertEquals(listOf("alpha", "Beta", "Zeta"), AppFilter.visible(apps, "", false, emptySet()).map { it.label })
        assertEquals(listOf("Zeta", "alpha", "Beta"), AppFilter.visible(apps, "", false, setOf("com.z")).map { it.label })
        assertEquals(4, AppFilter.visible(apps, "", true, emptySet()).size)
        assertEquals(listOf("Settings"), AppFilter.visible(apps, "SYS", true, emptySet()).map { it.label })
        // A chosen system app stays in the list so it can be unchosen even with the filter off.
        assertEquals("Settings", AppFilter.visible(apps, "", false, setOf("com.sys")).first().label)
    }

    // ---- backup rules and the core's error reasons

    @Test fun passwordRulesMatchTheDesktopDialog() {
        assertEquals(PasswordIssue.Short, BackupRules.issue("1234567"))
        assertNull(BackupRules.issue("12345678"))
        assertNull(BackupRules.issue("пароль-пароль"))
        assertEquals(PasswordIssue.Long, BackupRules.issue("я".repeat(600)))
        // NFC: "é" typed as e + combining accent is the same password as the single character.
        assertTrue(BackupRules.same("café-key", "café-key"))
        assertEquals("café", BackupRules.normalize("café"))
    }

    @Test fun backupErrorsAreWordedByTheCoresReason() {
        assertEquals(R.string.bk_err_wrongPassword, BackupErrors.text("backup_wrong_password", ErrorKind.Generic))
        assertEquals(R.string.bk_err_connected, BackupErrors.text("connection_active", ErrorKind.Precondition))
        assertEquals(R.string.err_generic, BackupErrors.text(null, ErrorKind.Generic))
        assertEquals(R.string.err_generic, BackupErrors.text("something_new", ErrorKind.Generic))
    }

    /** google.rpc.ErrorInfo { reason = 1, domain = 2 } as the core sends it inside a Connect error. */
    private fun errorInfo(reason: String, domain: String): String {
        fun field(n: Int, s: String) = byteArrayOf(((n shl 3) or 2).toByte(), s.length.toByte()) + s.toByteArray()
        return Base64.getEncoder().withoutPadding().encodeToString(field(1, reason) + field(2, domain))
    }

    @Test fun theReasonIsReadFromTheErrorDetails() {
        val body = """{"code":"permission_denied","message":"x","details":[{"type":"google.rpc.ErrorInfo","value":"${errorInfo("backup_wrong_password", "lavray.core")}"}]}"""
        val e: ApiException = errorFromBody(body.toByteArray(), 403)
        assertEquals(ConnectCode.PERMISSION_DENIED, e.code)
        assertEquals("backup_wrong_password", e.reason)
    }

    @Test fun aForeignDomainOrDamagedDetailGivesNoReason() {
        val foreign = """{"code":"internal","message":"x","details":[{"type":"google.rpc.ErrorInfo","value":"${errorInfo("backup_busy", "other.domain")}"}]}"""
        assertNull(errorFromBody(foreign.toByteArray(), 500).reason)
        val damaged = """{"code":"internal","message":"x","details":[{"type":"google.rpc.ErrorInfo","value":"////"},{"type":"other","value":"AAAA"}]}"""
        assertNull(errorFromBody(damaged.toByteArray(), 500).reason)
        val none = """{"code":"internal","message":"x"}"""
        assertNull(errorFromBody(none.toByteArray(), 500).reason)
    }
}
