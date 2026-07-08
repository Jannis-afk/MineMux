package com.termux.app.minemux;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.util.Log;

public class MineMuxBootReceiver extends BroadcastReceiver {

    private static final String TAG = "MineMuxBoot";

    @Override
    public void onReceive(Context context, Intent intent) {
        if (!Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction())) return;

        Log.i(TAG, "Boot completed; starting MineMux daemon");
        MineMuxRuntime.startDaemonFromBoot(context.getApplicationContext());
    }
}
