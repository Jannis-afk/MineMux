package com.termux.app.minemux;

import android.app.Activity;
import android.content.Intent;
import android.os.Bundle;
import android.view.Gravity;
import android.view.ViewGroup;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.Nullable;

import com.termux.app.TermuxActivity;

public class MineMuxActivity extends Activity {

    private WebView webView;

    @Override
    protected void onCreate(@Nullable Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        MineMuxRuntime.ensureInstalled(this);
        MineMuxRuntime.startDaemon(this);

        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setBackgroundColor(0xff101412);

        LinearLayout toolbar = new LinearLayout(this);
        toolbar.setGravity(Gravity.CENTER_VERTICAL);
        toolbar.setPadding(12, 10, 12, 10);
        toolbar.setBackgroundColor(0xff151b18);

        TextView title = new TextView(this);
        title.setText("MineMux");
        title.setTextColor(0xffedf5ef);
        title.setTextSize(20);
        title.setGravity(Gravity.CENTER_VERTICAL);
        toolbar.addView(title, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));

        Button start = new Button(this);
        start.setText("Start");
        start.setOnClickListener(v -> {
            MineMuxRuntime.ensureInstalled(this);
            MineMuxRuntime.startDaemon(this);
            Toast.makeText(this, "Starting MineMux daemon", Toast.LENGTH_SHORT).show();
            webView.postDelayed(() -> webView.loadUrl(MineMuxRuntime.DASHBOARD_URL), 1200);
        });
        toolbar.addView(start);

        Button terminal = new Button(this);
        terminal.setText("Terminal");
        terminal.setOnClickListener(v -> startActivity(new Intent(this, TermuxActivity.class)));
        toolbar.addView(terminal);

        webView = new WebView(this);
        WebSettings settings = webView.getSettings();
        settings.setJavaScriptEnabled(true);
        settings.setDomStorageEnabled(true);
        webView.setWebViewClient(new WebViewClient());
        webView.loadUrl(MineMuxRuntime.DASHBOARD_URL);

        root.addView(toolbar, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        root.addView(webView, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1));
        setContentView(root);
    }
}
